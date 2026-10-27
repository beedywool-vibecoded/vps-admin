package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/proxy"
	"github.com/beedywool/vps-admin/internal/store"
)

// Order the Proxy list was dragged into. Paused apps are tracked by internal/deploy.
const proxyOrderSetting = "proxy.order"

// proxySites lists every domain the reverse proxy serves. A managed snippet whose app is
// gone loses its id so the panel shows it as left over.
func (s *Server) proxySites(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.proxyView(r))
}

func (s *Server) proxyView(r *http.Request) proxy.Report {
	ctx := r.Context()
	rep := proxy.Read(s.Cfg.CaddySitesDir, s.Cfg.NginxSitesDir)
	apps := map[string]store.App{}
	if s.Store != nil {
		if list, err := s.Store.Apps(ctx); err == nil {
			for _, a := range list {
				apps[a.ID] = a
			}
		}
	}
	for i := range rep.Sites {
		site := &rep.Sites[i]
		if !site.Managed {
			continue
		}
		if s.Store != nil {
			if _, ok := apps[site.ID]; !ok {
				site.ID = ""
				continue
			}
		}
		site.Paused = s.Deploy != nil && s.Deploy.Paused(ctx, site.ID)
	}
	// Paused site files are parsed from their kept-aside copy.
	if s.Deploy != nil {
		for _, p := range s.Deploy.PausedSites() {
			dir := s.Cfg.CaddySitesDir
			if p.Engine == "nginx" {
				dir = s.Cfg.NginxSitesDir
			}
			rows := proxy.ReadFile(p.Stored, p.File, p.Engine, dir)
			if len(rows) == 0 {
				rows = []proxy.Site{{File: p.File}}
			}
			for _, row := range rows {
				row.Paused = true
				if _, ok := apps[row.ID]; !ok {
					row.ID = ""
				}
				rep.Sites = append(rep.Sites, row)
			}
		}
	}
	// Paused apps have no snippet but stay listed so they can be resumed.
	if s.Deploy != nil {
		for id, app := range apps {
			if app.Domain == "" || !s.Deploy.Paused(ctx, id) {
				continue
			}
			if a := deploy.Access(app); a != deploy.AccessPublic && a != deploy.AccessVPN {
				continue
			}
			if slices.ContainsFunc(rep.Sites, func(x proxy.Site) bool { return x.ID == id }) {
				continue
			}
			rep.Sites = append(rep.Sites, proxy.Site{
				Domains: []string{app.Domain},
				Target:  fmt.Sprintf("127.0.0.1:%d", s.Deploy.LivePort(ctx, app)),
				File:    s.snippetPath(rep.Engine, id),
				Managed: true,
				ID:      id,
				Scheme:  "https",
				VPNOnly: deploy.Access(app) == deploy.AccessVPN,
				Paused:  true,
			})
		}
	}
	// Drop .bak rows too, including paused copies kept by older versions.
	if rep.Engine == "nginx" {
		rep.Sites = slices.DeleteFunc(rep.Sites, func(site proxy.Site) bool {
			return strings.Contains(strings.ToLower(filepath.Base(site.File)), ".bak")
		})
	}
	s.orderSites(ctx, rep.Sites)
	return rep
}

func (s *Server) snippetPath(engine, id string) string {
	if engine == "nginx" {
		return filepath.Join(s.Cfg.NginxSitesDir, id+".conf")
	}
	return filepath.Join(s.Cfg.CaddySitesDir, id+".caddy")
}

// siteKey is the app id for a managed site, else file and first domain.
func siteKey(s proxy.Site) string {
	if s.ID != "" {
		return "app:" + s.ID
	}
	d := ""
	if len(s.Domains) > 0 {
		d = s.Domains[0]
	}
	return "file:" + s.File + "|" + d
}

// orderSites applies the dragged order; unknown sites go last in domain order.
func (s *Server) orderSites(ctx context.Context, sites []proxy.Site) {
	if s.Store == nil {
		return
	}
	var order []string
	json.Unmarshal([]byte(s.Store.Setting(ctx, proxyOrderSetting, "[]")), &order)
	if len(order) == 0 {
		return
	}
	rank := make(map[string]int, len(order))
	for i, k := range order {
		rank[k] = i
	}
	sort.SliceStable(sites, func(i, j int) bool {
		ri, oki := rank[siteKey(sites[i])]
		rj, okj := rank[siteKey(sites[j])]
		switch {
		case oki && okj:
			return ri < rj
		case oki:
			return true
		default:
			return false
		}
	})
}

// proxyPause withdraws or restores an app's URL. Its containers are left alone.
func (s *Server) proxyPause(w http.ResponseWriter, r *http.Request) {
	var body struct {
		App    string `json:"app"`
		Paused bool   `json:"paused"`
	}
	if err := readJSON(r, &body); err != nil || body.App == "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "an app is expected")
		return
	}
	if s.Deploy == nil {
		writeError(w, http.StatusConflict, "no_proxy")
		return
	}
	app, err := s.Store.App(r.Context(), body.App)
	if errors.Is(err, store.ErrNotFound) {
		writeErrorMsg(w, http.StatusNotFound, "not_found", "this app does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	// Only proxied apps have a URL to pause; otherwise the flag could never be cleared.
	if a := deploy.Access(*app); app.Domain == "" || (a != deploy.AccessPublic && a != deploy.AccessVPN) {
		writeErrorMsg(w, http.StatusBadRequest, "no_url", "this app has no proxy URL to pause")
		return
	}
	err = s.Deploy.SetPaused(r.Context(), *app, body.Paused)
	s.audit(r, "proxy.pause", body.App+" "+strconv.FormatBool(body.Paused), err)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "proxy_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.proxyView(r))
}

func (s *Server) proxyOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order []string `json:"order"`
	}
	if err := readJSON(r, &body); err != nil || len(body.Order) > 500 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	for _, k := range body.Order {
		if k == "" || len(k) > 512 {
			writeErrorMsg(w, http.StatusBadRequest, "invalid", "a site key is a short string")
			return
		}
	}
	b, _ := json.Marshal(body.Order)
	err := s.Store.SetSetting(r.Context(), proxyOrderSetting, string(b))
	s.audit(r, "proxy.order", "", err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, s.proxyView(r))
}

// proxySite pauses, resumes or deletes one site, whoever wrote it. Domain selects the
// block in a shared Caddyfile.
func (s *Server) proxySite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		File   string `json:"file"`
		Action string `json:"action"` // pause, resume, delete or https
		Domain string `json:"domain"` // which site in a shared Caddyfile, or where a certificate is asked for
	}
	if err := readJSON(r, &body); err != nil || body.File == "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "a file is expected")
		return
	}
	if s.Deploy == nil {
		writeError(w, http.StatusConflict, "no_proxy")
		return
	}
	var err error
	switch body.Action {
	case "pause":
		err = s.Deploy.PauseSite(r.Context(), body.File, body.Domain)
	case "resume":
		err = s.Deploy.ResumeSite(r.Context(), body.File, body.Domain)
	case "delete":
		err = s.Deploy.DeleteSite(r.Context(), body.File, body.Domain)
	case "https":
		err = s.Deploy.EnableHTTPS(r.Context(), body.File, body.Domain)
	default:
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "action must be pause, resume, delete or https")
		return
	}
	target := body.File
	if body.Domain != "" {
		target += " " + body.Domain
	}
	s.audit(r, "proxy.site_"+body.Action, target, err)
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "site_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.proxyView(r))
}
