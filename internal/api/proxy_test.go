package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/proxy"
	"github.com/beedywool/vps-admin/internal/store"
)

// proxyServer builds a panel whose Caddy config lives in a temporary root, with an app
// the panel publishes.
func proxyServer(t *testing.T) (*Server, *deploy.Engine, string) {
	t.Helper()
	root := t.TempDir()
	sites := filepath.Join(root, "caddy", "vps-admin-apps")
	if err := os.MkdirAll(sites, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "caddy", "Caddyfile"),
		[]byte("import vps-admin-apps/*.caddy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	cfg.CaddySitesDir = sites
	eng := &deploy.Engine{
		Store: st, DataDir: t.TempDir(), SitesDir: sites,
		ReloadCaddy: func(context.Context) error { return nil },
	}
	return &Server{Cfg: cfg, Store: st, Deploy: eng}, eng, sites
}

func addApp(t *testing.T, s *Server, app store.App) store.App {
	t.Helper()
	if err := s.Store.AddApp(context.Background(), &app); err != nil {
		t.Fatal(err)
	}
	return app
}

func post(t *testing.T, h http.HandlerFunc, s *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
	return rec
}

func sites(t *testing.T, rec *httptest.ResponseRecorder) proxy.Report {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var rep proxy.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

// Pausing takes the domain out of the proxy, keeps the app in the list, and turning it
// back on writes the snippet again with the app's port.
func TestProxyPauseAndResume(t *testing.T) {
	s, _, dir := proxyServer(t)
	app := addApp(t, s, store.App{
		ID: "web", Domain: "web.example.com", Access: deploy.AccessPublic,
		Build: deploy.BuildCompose, Port: 3000,
	})
	// Resuming an app that was never paused publishes it: the same path a fix uses.
	if rec := post(t, s.proxyPause, s, "/api/proxy/pause", fmt.Sprintf(`{"app":%q,"paused":false}`, app.ID)); rec.Code != http.StatusOK {
		t.Fatalf("publish = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "web.caddy")); err != nil {
		t.Fatalf("the site was not written: %v", err)
	}

	rep := sites(t, post(t, s.proxyPause, s, "/api/proxy/pause", fmt.Sprintf(`{"app":%q,"paused":true}`, app.ID)))
	if _, err := os.Stat(filepath.Join(dir, "web.caddy")); !os.IsNotExist(err) {
		t.Fatalf("the snippet survived the pause: %v", err)
	}
	if len(rep.Sites) != 1 || !rep.Sites[0].Paused || rep.Sites[0].ID != "web" {
		t.Fatalf("the paused site left the list: %+v", rep.Sites)
	}
	if rep.Sites[0].Target != "127.0.0.1:3000" {
		t.Fatalf("target = %q", rep.Sites[0].Target)
	}

	rep = sites(t, post(t, s.proxyPause, s, "/api/proxy/pause", fmt.Sprintf(`{"app":%q,"paused":false}`, app.ID)))
	if len(rep.Sites) != 1 || rep.Sites[0].Paused {
		t.Fatalf("resume = %+v", rep.Sites)
	}
	conf, err := os.ReadFile(filepath.Join(dir, "web.caddy"))
	if err != nil || !strings.Contains(string(conf), "reverse_proxy 127.0.0.1:3000") {
		t.Fatalf("snippet after resume: %v %s", err, conf)
	}
}

// An app with no proxy URL cannot be paused: there would be nothing to turn back on.
func TestProxyPauseNeedsAURL(t *testing.T) {
	s, _, _ := proxyServer(t)
	addApp(t, s, store.App{ID: "local", Access: deploy.AccessLocal, Build: deploy.BuildCompose, Port: 3000})
	addApp(t, s, store.App{ID: "ts", Access: deploy.AccessTailscale, Domain: "box.tail.ts.net", Build: deploy.BuildCompose, Port: 3000})
	for _, id := range []string{"local", "ts", "missing"} {
		rec := post(t, s.proxyPause, s, "/api/proxy/pause", fmt.Sprintf(`{"app":%q,"paused":true}`, id))
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 400 or 404", id, rec.Code)
		}
	}
}

func TestProxyOrderIsKept(t *testing.T) {
	s, _, _ := proxyServer(t)
	addApp(t, s, store.App{ID: "aaa", Domain: "aaa.example.com", Access: deploy.AccessPublic, Build: deploy.BuildCompose, Port: 3000})
	addApp(t, s, store.App{ID: "zzz", Domain: "zzz.example.com", Access: deploy.AccessPublic, Build: deploy.BuildCompose, Port: 3000})
	for _, id := range []string{"aaa", "zzz"} {
		post(t, s.proxyPause, s, "/api/proxy/pause", fmt.Sprintf(`{"app":%q,"paused":false}`, id))
	}
	rec := sites(t, post(t, s.proxyOrder, s, "/api/proxy/order", `{"order":["app:zzz","app:aaa"]}`))
	if len(rec.Sites) != 2 || rec.Sites[0].ID != "zzz" || rec.Sites[1].ID != "aaa" {
		t.Fatalf("order = %+v", rec.Sites)
	}
	// A new site nobody ordered stays after the ordered ones.
	addApp(t, s, store.App{ID: "mmm", Domain: "mmm.example.com", Access: deploy.AccessPublic, Build: deploy.BuildCompose, Port: 3000})
	post(t, s.proxyPause, s, "/api/proxy/pause", `{"app":"mmm","paused":false}`)
	rec = sites(t, post(t, s.proxyOrder, s, "/api/proxy/order", `{"order":["app:zzz","app:aaa"]}`))
	if len(rec.Sites) != 3 || rec.Sites[2].ID != "mmm" {
		t.Fatalf("order = %+v", rec.Sites)
	}
}

// Any site file in the proxy's directory can be paused and put back, and deleted,
// whoever wrote it: the panel's snippet, a block added by hand, a file left by a gone app.
func TestProxySitePauseResumeAndDelete(t *testing.T) {
	s, eng, dir := proxyServer(t)
	reloads := 0
	eng.ReloadCaddy = func(context.Context) error { reloads++; return nil }
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "hand.caddy")
	body := "hand.example.com {\n\treverse_proxy 127.0.0.1:9000\n}\n"
	write(path, body)
	call := func(action string) *httptest.ResponseRecorder {
		return post(t, s.proxySite, s, "/api/proxy/site", fmt.Sprintf(`{"file":%q,"action":%q}`, path, action))
	}

	rep := sites(t, call("pause"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the site file survived the pause: %v", err)
	}
	if len(rep.Sites) != 1 || !rep.Sites[0].Paused {
		t.Fatalf("the paused site left the list: %+v", rep.Sites)
	}
	if d := rep.Sites[0].Domains; len(d) != 1 || d[0] != "hand.example.com" || rep.Sites[0].Target != "127.0.0.1:9000" {
		t.Fatalf("the paused row lost its details: %+v", rep.Sites[0])
	}
	if reloads != 1 {
		t.Fatalf("Caddy was reloaded %d times, want 1", reloads)
	}

	rep = sites(t, call("resume"))
	if got, err := os.ReadFile(path); err != nil || string(got) != body {
		t.Fatalf("the site came back changed: %v %q", err, got)
	}
	if len(rep.Sites) != 1 || rep.Sites[0].Paused {
		t.Fatalf("resume = %+v", rep.Sites)
	}

	rep = sites(t, call("delete"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the deleted site is still there: %v", err)
	}
	if len(rep.Sites) != 0 {
		t.Fatalf("the deleted site is still listed: %+v", rep.Sites)
	}

	// A paused site can be deleted from the list just as well.
	write(path, body)
	call("pause")
	sites(t, call("delete"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("deleting a paused site brought it back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(eng.DataDir, "proxy-paused", "caddy__hand.caddy")); !os.IsNotExist(err) {
		t.Fatalf("the kept copy survived the delete: %v", err)
	}
}

// A file an app still owns is driven from the app: deleting or pausing it here would be
// undone by the next deployment, or leave the app with no route and no way back.
func TestProxySiteRefusesAnAppsSite(t *testing.T) {
	s, _, dir := proxyServer(t)
	addApp(t, s, store.App{ID: "live", Domain: "live.example.com", Access: deploy.AccessPublic, Build: deploy.BuildCompose, Port: 3000})
	path := filepath.Join(dir, "live.caddy")
	if err := os.WriteFile(path, []byte("# Managed by VPS Admin (app live), rewritten on every deploy\nlive.example.com {\n\treverse_proxy 127.0.0.1:3000\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"pause", "delete"} {
		rec := post(t, s.proxySite, s, "/api/proxy/site", fmt.Sprintf(`{"file":%q,"action":%q}`, path, action))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "live") {
			t.Errorf("%s: %d %s", action, rec.Code, rec.Body.String())
		}
	}
	// An unknown action is refused before anything is touched.
	if rec := post(t, s.proxySite, s, "/api/proxy/site", fmt.Sprintf(`{"file":%q,"action":"rename"}`, path)); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown action: %d", rec.Code)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("the app's site was touched")
	}
}

// Nothing outside the two directories the proxy reads is reachable from the panel.
func TestProxySiteStaysInTheSiteDirs(t *testing.T) {
	s, _, dir := proxyServer(t)
	for _, file := range []string{
		filepath.Join(dir, "..", "Caddyfile"),
		filepath.Join(dir, "sub", "deep.caddy"),
		"/etc/passwd",
		filepath.Join(dir, "notes.txt"),
	} {
		rec := post(t, s.proxySite, s, "/api/proxy/site", fmt.Sprintf(`{"file":%q,"action":"delete"}`, file))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", file, rec.Code)
		}
	}
}

// The same three actions on nginx: the file moves out of the sites directory, the root
// helper reloads nginx, and nothing else is touched.
func TestProxySiteOnNginx(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "vps-admin-apps")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	cfg.CaddySitesDir = ""
	cfg.NginxSitesDir = dir
	var calls []string
	eng := &deploy.Engine{
		Store: st, DataDir: t.TempDir(), NginxDir: dir,
		Helper: func(_ context.Context, args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			return "", nil
		},
	}
	s := &Server{Cfg: cfg, Store: st, Deploy: eng}

	path := filepath.Join(dir, "blog.conf")
	body := "server {\n\tlisten 80;\n\tserver_name blog.example.com;\n\tlocation / { proxy_pass http://127.0.0.1:4000; }\n}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	call := func(action string) *httptest.ResponseRecorder {
		return post(t, s.proxySite, s, "/api/proxy/site", fmt.Sprintf(`{"file":%q,"action":%q}`, path, action))
	}

	rep := sites(t, call("pause"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the nginx site survived the pause: %v", err)
	}
	if len(rep.Sites) != 1 || !rep.Sites[0].Paused || rep.Sites[0].Domains[0] != "blog.example.com" {
		t.Fatalf("paused nginx row = %+v", rep.Sites)
	}

	sites(t, call("resume"))
	if got, err := os.ReadFile(path); err != nil || string(got) != body {
		t.Fatalf("nginx site after resume: %v %q", err, got)
	}
	if len(calls) != 2 || calls[0] != "nginx-reload" || calls[1] != "nginx-reload" {
		t.Fatalf("helper calls = %v, want two nginx-reload", calls)
	}
}

// A proxy that refuses the new configuration keeps the site: pause writes the file back
// and the panel reports the refusal instead of the site disappearing.
func TestProxySiteRestoresWhenTheProxyRefuses(t *testing.T) {
	s, eng, dir := proxyServer(t)
	eng.ReloadCaddy = func(context.Context) error { return errors.New("bad config") }
	path := filepath.Join(dir, "hand.caddy")
	body := "hand.example.com {\n\tfile_server\n}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := post(t, s.proxySite, s, "/api/proxy/site", fmt.Sprintf(`{"file":%q,"action":"pause"}`, path))
	if rec.Code == http.StatusOK {
		t.Fatal("the pause was accepted")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != body {
		t.Fatalf("the site was not put back: %v %q", err, got)
	}
}

// Deleting an app withdraws its Caddy site in the same request, and frees its paused
// flag so the id can be used again.
func TestDeleteAppWithdrawsItsSite(t *testing.T) {
	s, _, dir := proxyServer(t)
	// A linked folder skips the docker CLI, which the test machine may not have.
	app := addApp(t, s, store.App{
		ID: "blog", Dir: t.TempDir(), Domain: "blog.example.com",
		Access: deploy.AccessPublic, Build: deploy.BuildCompose, Port: 3000,
	})
	post(t, s.proxyPause, s, "/api/proxy/pause", `{"app":"blog","paused":false}`)
	path := filepath.Join(dir, "blog.caddy")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("site was not published: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/apps/blog", nil)
	req.SetPathValue("id", app.ID)
	s.appDelete(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the Caddy snippet outlived its app: %v", err)
	}
	if _, err := s.Store.App(context.Background(), "blog"); err == nil {
		t.Fatal("the app is still in the store")
	}
}
