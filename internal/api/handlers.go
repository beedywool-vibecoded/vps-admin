package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/beedywool/vps-admin/internal/auth"
	"github.com/beedywool/vps-admin/internal/buildinfo"
	"github.com/beedywool/vps-admin/internal/system"
	"github.com/beedywool/vps-admin/internal/update"
)

// Auth

func (s *Server) authState(w http.ResponseWriter, r *http.Request) {
	st, err := s.Auth.State(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
		Name  string `json:"name"`
		Owner string `json:"owner"`
		Kind  string `json:"kind"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ip := s.clientIP(r)
	res, err := s.Auth.Pair(r.Context(), w, ip, body.Code, body.Name, body.Owner, body.Kind)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited")
	case errors.Is(err, auth.ErrNoOwner):
		writeErrorMsg(w, http.StatusBadRequest, "owner_required", "Enter your name, so the other admins know who uses this device. If this is an old version of the app, update it first.")
	case errors.Is(err, auth.ErrBadCode):
		s.Store.Audit(r.Context(), auditEntry(ip, "auth.pair", body.Kind, err))
		writeError(w, http.StatusUnauthorized, "bad_code")
	case err != nil:
		writeErrorMsg(w, http.StatusBadRequest, "pair_failed", err.Error())
	default:
		e := auditEntry(ip, "auth.pair", body.Kind, nil)
		e.DeviceID = res.DeviceID
		s.Store.Audit(r.Context(), e)
		writeJSON(w, http.StatusOK, res)
	}
}

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	opts, err := s.Auth.PasskeyLoginBegin(r.Context())
	if err != nil {
		writeErrorMsg(w, http.StatusInternalServerError, "passkey_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	dev, err := s.Auth.PasskeyLoginFinish(r.Context(), w, r, ip, r.URL.Query().Get("ceremony"))
	e := auditEntry(ip, "auth.passkey_login", "", err)
	e.DeviceID = dev
	s.Store.Audit(r.Context(), e)
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited")
	case err != nil:
		writeError(w, http.StatusUnauthorized, "passkey_rejected")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"deviceId": dev})
	}
}

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	opts, err := s.Auth.PasskeyRegisterBegin(r.Context(), auth.FromContext(r.Context()))
	if err != nil {
		writeErrorMsg(w, http.StatusInternalServerError, "passkey_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	err := s.Auth.PasskeyRegisterFinish(r.Context(), w, r, auth.FromContext(r.Context()), r.URL.Query().Get("ceremony"))
	s.audit(r, "auth.passkey_register", "", err)
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "passkey_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(r.Context(), w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Devices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": list, "current": auth.FromContext(r.Context()).DeviceID})
}

func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	owner, name := auth.CleanName(body.Owner, ""), auth.CleanName(body.Name, "")
	if owner == "" || name == "" {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "both the person and the device name are needed")
		return
	}
	id := r.PathValue("id")
	err := s.Store.RenameDevice(r.Context(), id, owner, name)
	s.audit(r, "device.rename", owner+" · "+name, err)
	if err != nil {
		writeError(w, statusFor(err), "rename_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.Store.RevokeDevice(r.Context(), id)
	s.audit(r, "device.revoke", id, err)
	if err != nil {
		writeError(w, statusFor(err), "revoke_failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// System

func (s *Server) systemInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"host": s.Host, "snapshot": s.Collector.Latest(), "version": buildinfo.Version,
	})
}

func (s *Server) systemHistory(w http.ResponseWriter, r *http.Request) {
	span := r.URL.Query().Get("span")
	if span != "24h" {
		span = "1h"
	}
	writeJSON(w, http.StatusOK, map[string]any{"span": span, "points": s.Collector.History(span)})
}

// systemStream pushes a snapshot every 5 seconds as server-sent events.
func (s *Server) systemStream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{}) // the server-wide write timeout would cut the stream
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Collector.Subscribe()
	defer cancel()
	send := func(v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send(s.Collector.Latest()) {
		return
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case snap := <-ch:
			if !send(snap) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	list, err := s.Runner.Services(r.Context())
	if err != nil {
		writeErrorMsg(w, http.StatusInternalServerError, "services_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": list})
}

func (s *Server) serviceAction(w http.ResponseWriter, r *http.Request) {
	name, action := r.PathValue("name"), r.PathValue("action")
	err := s.Runner.ServiceAction(r.Context(), name, action)
	s.audit(r, "service."+action, name, err)
	if err != nil {
		writeErrorMsg(w, statusFor(err), "action_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serviceLogs(w http.ResponseWriter, r *http.Request) {
	out, err := s.Runner.ServiceLogs(r.Context(), r.PathValue("name"), queryInt(r, "lines", 200))
	if err != nil {
		writeErrorMsg(w, statusFor(err), "logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": out})
}

// securityReport caches the audit for a minute: it runs a dozen commands.
func (s *Server) securityReport(w http.ResponseWriter, r *http.Request) {
	s.secMu.Lock()
	defer s.secMu.Unlock()
	if s.security == nil || time.Since(s.security.Checked) > time.Minute || r.URL.Query().Get("refresh") == "1" {
		rep := s.Runner.Audit(r.Context())
		s.addDockerCheck(r, &rep)
		s.security = &rep
	}
	writeJSON(w, http.StatusOK, s.security)
}

// addDockerCheck flags published container ports, which bypass ufw.
func (s *Server) addDockerCheck(r *http.Request, rep *system.Security) {
	list, err := s.Docker.Containers(r.Context())
	if err != nil {
		return
	}
	c := system.Check{ID: "docker_ports", Group: "network", Title: "Published container ports", Status: system.OK,
		Detail: "No container publishes a port on every interface"}
	var exposed []string
	for _, ct := range list {
		if !ct.Exposed || ct.State != "running" {
			continue
		}
		for _, p := range ct.Ports {
			if p.PublicPort != 0 && (p.IP == "0.0.0.0" || p.IP == "::" || p.IP == "") {
				exposed = append(exposed, fmt.Sprintf("%s:%d", ct.Name, p.PublicPort))
			}
		}
	}
	if len(exposed) > 0 {
		c.Status = system.Warn
		c.Detail = "Reachable from the internet whatever ufw says: " + strings.Join(exposed, ", ")
		c.Fix = "Publish on 127.0.0.1 (\"127.0.0.1:8080:80\") and serve through the reverse proxy, unless the port must be public."
	}
	rep.Checks = append(rep.Checks, c)
}

// Docker

func (s *Server) containers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Docker.Containers(r.Context())
	if err != nil {
		writeErrorMsg(w, statusFor(err), "docker_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": list})
}

func (s *Server) containerStats(w http.ResponseWriter, r *http.Request) {
	list, err := s.Docker.Containers(r.Context())
	if err != nil {
		writeErrorMsg(w, statusFor(err), "docker_failed", err.Error())
		return
	}
	var ids []string
	for _, c := range list {
		if c.State == "running" {
			ids = append(ids, c.ID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": s.Docker.Stats(r.Context(), ids)})
}

func (s *Server) containerAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	err := s.Docker.Action(r.Context(), id, action)
	s.audit(r, "container."+action, id, err)
	if err != nil {
		writeErrorMsg(w, statusFor(err), "action_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	out, err := s.Docker.Logs(r.Context(), r.PathValue("id"), queryInt(r, "tail", 200))
	if err != nil {
		writeErrorMsg(w, statusFor(err), "logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": out})
}

// Update

type updateView struct {
	update.Status
	Auto       bool `json:"auto"`
	Prerelease bool `json:"prerelease"`
}

func (s *Server) updateView(r *http.Request, st update.Status) updateView {
	return updateView{Status: st, Auto: s.autoUpdate(r.Context()), Prerelease: s.prerelease(r.Context())}
}

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.updateView(r, s.Updater.Status()))
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	st, err := s.Updater.Check(r.Context(), s.prerelease(r.Context()))
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "check_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.updateView(r, st))
}

func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	st := s.Updater.Status()
	if !st.Available || st.Latest == nil {
		writeError(w, http.StatusConflict, "no_update")
		return
	}
	err := s.Updater.Apply(r.Context(), st.Latest)
	s.audit(r, "update.apply", st.Latest.Tag, err)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"installed": st.Latest.Tag})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(500 * time.Millisecond) // let the response reach the client
		s.Restart()
	}()
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Auto       *bool `json:"auto"`
		Prerelease *bool `json:"prerelease"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx := r.Context()
	if body.Auto != nil {
		s.Store.SetSetting(ctx, "update.auto", strconv.FormatBool(*body.Auto))
	}
	if body.Prerelease != nil {
		s.Store.SetSetting(ctx, "update.prerelease", strconv.FormatBool(*body.Prerelease))
	}
	s.audit(r, "settings.update", "", nil)
	writeJSON(w, http.StatusOK, s.updateView(r, s.Updater.Status()))
}

func (s *Server) auditLog(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.AuditLog(r.Context(), int64(queryInt(r, "before", 0)), min(max(queryInt(r, "limit", 50), 1), 200))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": list})
}

// Android app updates, for paired phones.

func (s *Server) appLatest(w http.ResponseWriter, r *http.Request) {
	meta, _, err := s.Updater.AppLatest(r.Context())
	if errors.Is(err, update.ErrNoRelease) {
		writeError(w, http.StatusNotFound, "no_release")
		return
	}
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "check_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(meta)
}

func (s *Server) appAPK(w http.ResponseWriter, r *http.Request) {
	_, rel, err := s.Updater.AppLatest(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, "no_release")
		return
	}
	res, err := s.Updater.AppAPK(r.Context(), rel)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "download_failed", err.Error())
		return
	}
	defer res.Body.Close()
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{}) // large file on a slow phone connection
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="vps-admin-`+rel.Tag+`.apk"`)
	if res.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(res.ContentLength, 10))
	}
	io.Copy(w, res.Body)
}
