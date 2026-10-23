// Package api exposes the agent over HTTP: JSON endpoints under /api, the panel elsewhere.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beedywool/vps-admin/internal/alerts"
	"github.com/beedywool/vps-admin/internal/assistant"
	"github.com/beedywool/vps-admin/internal/auth"
	"github.com/beedywool/vps-admin/internal/buildinfo"
	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/github"
	"github.com/beedywool/vps-admin/internal/scripts"
	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
	"github.com/beedywool/vps-admin/internal/system"
	"github.com/beedywool/vps-admin/internal/update"
	"github.com/beedywool/vps-admin/internal/web"
)

type Server struct {
	Cfg       *config.Config
	Store     *store.Store
	Auth      *auth.Auth
	Collector *system.Collector
	Runner    *system.Runner
	Docker    *docker.Client
	Updater   *update.Updater
	Box       *secret.Box
	GitHub    *github.Client
	Deploy    *deploy.Engine
	Assistant *assistant.Engine
	Scripts   *scripts.Runner
	Alerts    *alerts.Watcher
	Host      system.Host
	// Restart is called after an update is installed; the process exits and systemd
	// starts the new binary.
	Restart func()

	secMu    sync.Mutex
	security *system.Security
	manifest manifestStates
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	full := func(h http.HandlerFunc) http.Handler { return s.Auth.Require(auth.ScopeFull, s.clientIP, h) }
	enroll := func(h http.HandlerFunc) http.Handler { return s.Auth.Require(auth.ScopeEnroll, s.clientIP, h) }

	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/auth/state", s.authState)
	mux.HandleFunc("POST /api/auth/pair", s.pair)
	mux.HandleFunc("POST /api/auth/passkey/login/begin", s.passkeyLoginBegin)
	mux.HandleFunc("POST /api/auth/passkey/login/finish", s.passkeyLoginFinish)
	mux.Handle("POST /api/auth/passkey/register/begin", enroll(s.passkeyRegisterBegin))
	mux.Handle("POST /api/auth/passkey/register/finish", enroll(s.passkeyRegisterFinish))
	mux.Handle("POST /api/auth/logout", enroll(s.logout))

	mux.Handle("GET /api/devices", full(s.devices))
	mux.Handle("DELETE /api/devices/{id}", full(s.revokeDevice))
	mux.Handle("PATCH /api/devices/{id}", full(s.renameDevice))

	mux.Handle("GET /api/system", full(s.systemInfo))
	mux.Handle("GET /api/system/history", full(s.systemHistory))
	mux.Handle("GET /api/system/stream", full(s.systemStream))
	mux.Handle("GET /api/services", full(s.services))
	mux.Handle("POST /api/services/{name}/{action}", full(s.serviceAction))
	mux.Handle("GET /api/services/{name}/logs", full(s.serviceLogs))
	mux.Handle("GET /api/security", full(s.securityReport))
	mux.Handle("GET /api/proxy", full(s.proxySites))
	mux.Handle("POST /api/proxy/pause", full(s.proxyPause))
	mux.Handle("POST /api/proxy/order", full(s.proxyOrder))
	mux.Handle("POST /api/proxy/site", full(s.proxySite))

	mux.Handle("GET /api/docker/containers", full(s.containers))
	mux.Handle("GET /api/docker/stats", full(s.containerStats))
	mux.Handle("POST /api/docker/containers/{id}/{action}", full(s.containerAction))
	mux.Handle("POST /api/docker/containers/{id}/rebuild", full(s.containerRebuild))
	mux.Handle("GET /api/docker/containers/{id}/logs", full(s.containerLogs))

	mux.Handle("GET /api/update", full(s.updateStatus))
	mux.Handle("POST /api/update/check", full(s.updateCheck))
	mux.Handle("POST /api/update/apply", full(s.updateApply))
	mux.Handle("PUT /api/settings/update", full(s.updateSettings))
	mux.Handle("GET /api/app/latest", full(s.appLatest))
	mux.Handle("GET /api/app/apk", full(s.appAPK))

	mux.Handle("GET /api/audit", full(s.auditLog))
	mux.Handle("POST /api/system/reboot", full(s.reboot))
	mux.Handle("GET /api/system/environment", full(s.systemEnvironment))
	mux.Handle("GET /api/appearance", full(s.appearance))
	mux.Handle("PUT /api/appearance", full(s.setAppearance))
	mux.Handle("PUT /api/appearance/icons", full(s.setGroupIcon))
	mux.Handle("PUT /api/appearance/titles", full(s.setGroupTitle))
	mux.Handle("GET /api/appearance/background", full(s.backgroundGet))
	mux.Handle("PUT /api/appearance/background", full(s.backgroundPut))
	mux.Handle("DELETE /api/appearance/background", full(s.backgroundDelete))

	mux.Handle("GET /api/alerts", full(s.alertsGet))
	mux.Handle("PUT /api/alerts", full(s.alertsPut))
	mux.Handle("POST /api/alerts/test", full(s.alertsTest))

	mux.Handle("GET /api/scripts", full(s.scriptsList))
	mux.Handle("POST /api/scripts/{name}/run", full(s.scriptRun))
	mux.Handle("PUT /api/scripts/{name}/title", full(s.scriptRename))
	mux.Handle("POST /api/scripts/{name}/stop", full(s.scriptStop))
	mux.Handle("GET /api/scripts/{name}/stream", full(s.scriptStream))
	mux.Handle("GET /api/script-runs/{run}", full(s.scriptRunOutput))

	mux.Handle("GET /api/github", full(s.githubStatus))
	mux.Handle("POST /api/github/manifest", full(s.githubManifest))
	mux.HandleFunc("GET /api/github/manifest/callback", s.githubCallback)
	mux.HandleFunc("POST /api/github/webhook", s.githubWebhook)
	mux.Handle("DELETE /api/github", full(s.githubDisconnect))
	mux.Handle("GET /api/github/repos", full(s.githubRepos))
	mux.Handle("GET /api/github/detect", full(s.githubDetect))
	mux.Handle("GET /api/github/branches", full(s.githubBranches))

	mux.Handle("GET /api/apps", full(s.appsList))
	mux.Handle("POST /api/apps", full(s.appCreate))
	mux.Handle("POST /api/apps/link", full(s.appLink))
	mux.Handle("GET /api/folders/inspect", full(s.folderInspect))
	mux.Handle("GET /api/apps/{id}/check", full(s.appCheck))
	mux.Handle("GET /api/apps/{id}", full(s.appGet))
	mux.Handle("PUT /api/apps/{id}", full(s.appUpdate))
	mux.Handle("DELETE /api/apps/{id}", full(s.appDelete))
	mux.Handle("PUT /api/apps/{id}/env", full(s.appSetEnv))
	mux.Handle("POST /api/apps/{id}/deploy", full(s.appDeploy))
	mux.Handle("POST /api/apps/{id}/restart", full(s.appRestart))
	mux.Handle("POST /api/apps/{id}/rollback/{dep}", full(s.appRollback))
	mux.Handle("GET /api/apps/{id}/deployments/{dep}", full(s.deploymentLog))
	mux.Handle("GET /api/apps/{id}/deployments/{dep}/stream", full(s.deploymentStream))

	mux.Handle("GET /api/assistant", full(s.assistantSettings))
	mux.Handle("PUT /api/assistant", full(s.assistantUpdate))
	mux.Handle("GET /api/assistant/conversations", full(s.conversations))
	mux.Handle("POST /api/assistant/conversations", full(s.conversationCreate))
	mux.Handle("GET /api/assistant/conversations/{id}", full(s.conversationGet))
	mux.Handle("DELETE /api/assistant/conversations/{id}", full(s.conversationDelete))
	mux.Handle("POST /api/assistant/conversations/{id}/messages", full(s.conversationSend))
	mux.Handle("POST /api/assistant/conversations/{id}/stop", full(s.conversationStop))
	mux.Handle("GET /api/assistant/conversations/{id}/stream", full(s.conversationStream))
	mux.Handle("POST /api/assistant/tools/{call}", full(s.toolDecide))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})
	mux.Handle("/", web.Handler())
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("X-Frame-Options", "DENY")
		// form-action allows github.com: connecting GitHub posts the App manifest there.
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self' https://github.com")
		hd.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			hd.Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}

// clientIP trusts X-Forwarded-For only from configured proxies, reading it right to
// left so a client cannot spoof its address by sending the header itself.
func (s *Server) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	if !s.Cfg.IsTrustedProxy(peer) {
		return peer.Unmap().String()
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !s.Cfg.IsTrustedProxy(a) {
			return a.Unmap().String()
		}
	}
	return peer.Unmap().String()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeErrorMsg(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func queryInt(r *http.Request, key string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(key)); err == nil {
		return v
	}
	return def
}

// audit records a state-changing action and logs failures.
func (s *Server) audit(r *http.Request, action, target string, err error) {
	e := store.AuditEntry{IP: s.clientIP(r), Action: action, Target: target, OK: err == nil}
	if id := auth.FromContext(r.Context()); id != nil {
		e.DeviceID = id.DeviceID
	}
	if err != nil {
		e.Detail = err.Error()
		slog.Warn("action failed", "action", action, "target", target, "err", err)
	}
	s.Store.Audit(r.Context(), e)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": buildinfo.Version})
}

// Update settings live in the database so the panel can change them; the config file
// only gives the defaults.
func (s *Server) autoUpdate(ctx context.Context) bool {
	return s.Store.Setting(ctx, "update.auto", strconv.FormatBool(s.Cfg.Update.Auto)) == "true"
}

func (s *Server) prerelease(ctx context.Context) bool {
	return s.Store.Setting(ctx, "update.prerelease", strconv.FormatBool(s.Cfg.Update.Prerelease)) == "true"
}

// AutoUpdateLoop checks GitHub every 6 hours and installs new releases when enabled. A
// version that preflight.sh had to roll back is never retried automatically.
func (s *Server) AutoUpdateLoop(ctx context.Context) {
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		timer.Reset(6 * time.Hour)
		st, err := s.Updater.Check(ctx, s.prerelease(ctx))
		if err != nil || !st.Available || !s.autoUpdate(ctx) || st.Latest.Tag == s.Updater.RolledBack() {
			continue
		}
		slog.Info("installing update", "from", st.Current, "to", st.Latest.Tag)
		err = s.Updater.Apply(ctx, st.Latest)
		s.Store.Audit(ctx, store.AuditEntry{Action: "update.auto", Target: st.Latest.Tag, OK: err == nil, Detail: errString(err)})
		if err == nil {
			s.Restart()
			return
		}
		slog.Error("update failed", "err", err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func statusFor(err error) int {
	switch {
	case errors.Is(err, docker.ErrNotFound), errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, docker.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, system.ErrBadUnit), errors.Is(err, system.ErrBadAction):
		return http.StatusBadRequest
	case errors.Is(err, system.ErrSelfLockout):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

func auditEntry(ip, action, target string, err error) store.AuditEntry {
	return store.AuditEntry{IP: ip, Action: action, Target: target, OK: err == nil, Detail: errString(err)}
}
