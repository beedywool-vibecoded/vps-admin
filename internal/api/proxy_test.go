package api

import (
	"context"
	"encoding/json"
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

// The left-over list: a snippet of a deleted app can be removed from the Proxy page,
// anything else is refused.
func TestProxyRemoveOrphan(t *testing.T) {
	s, _, dir := proxyServer(t)
	orphan := filepath.Join(dir, "gone.caddy")
	write := func(path string, body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(orphan, "# Managed by VPS Admin (app gone), rewritten on every deploy\ngone.example.com {\n\treverse_proxy 127.0.0.1:3000\n}\n")
	write(filepath.Join(dir, "hand.caddy"), "hand.example.com {\n\tfile_server\n}\n")
	addApp(t, s, store.App{ID: "live", Domain: "live.example.com", Access: deploy.AccessPublic, Build: deploy.BuildCompose, Port: 3000})
	write(filepath.Join(dir, "live.caddy"), "# Managed by VPS Admin (app live), rewritten on every deploy\nlive.example.com {\n\treverse_proxy 127.0.0.1:3000\n}\n")

	for _, c := range []struct{ file, why string }{
		{filepath.Join(dir, "hand.caddy"), "hand-written"},
		{filepath.Join(dir, "live.caddy"), "still serves an app"},
		{filepath.Join(dir, "..", "Caddyfile"), "outside the snippets"},
		{"/etc/passwd", "outside the snippets"},
	} {
		rec := post(t, s.proxyRemoveOrphan, s, "/api/proxy/orphans/remove", fmt.Sprintf(`{"file":%q}`, c.file))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c.why, rec.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "hand.caddy")); err != nil {
		t.Fatal("a hand-written site was removed")
	}

	rep := sites(t, post(t, s.proxyRemoveOrphan, s, "/api/proxy/orphans/remove", fmt.Sprintf(`{"file":%q}`, orphan)))
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("the orphan is still there: %v", err)
	}
	for _, x := range rep.Sites {
		if x.File == orphan {
			t.Fatalf("the left-over site is still listed: %+v", x)
		}
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
