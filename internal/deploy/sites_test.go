package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/store"
)

func testLogger() *logger { return &logger{e: &Engine{}} }

func TestNginxSiteVPNAndCertbot(t *testing.T) {
	dir := t.TempDir()
	var calls []string
	e := &Engine{NginxDir: dir, Helper: func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}}
	if e.Proxy() != "nginx" {
		t.Fatalf("proxy = %q", e.Proxy())
	}
	app := store.App{ID: "web", Domain: "web.example.com", Access: AccessVPN, Allow: "10.8.0.1/24, 192.168.1.7"}
	if err := e.publish(context.Background(), testLogger(), app, 35001); err != nil {
		t.Fatal(err)
	}
	conf, _ := os.ReadFile(filepath.Join(dir, "web.conf"))
	for _, want := range []string{"server_name web.example.com;", "allow 10.8.0.0/24;", "allow 192.168.1.7;", "deny all;", "proxy_pass http://127.0.0.1:35001;"} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("site lacks %q:\n%s", want, conf)
		}
	}
	if strings.Join(calls, "|") != "nginx-reload|certbot web.example.com" {
		t.Errorf("helper calls %v", calls)
	}
	// Going local withdraws the site.
	app.Access = AccessLocal
	calls = nil
	e.publish(context.Background(), testLogger(), app, 35001)
	if _, err := os.Stat(filepath.Join(dir, "web.conf")); err == nil {
		t.Error("site kept after switching to local")
	}
	if strings.Join(calls, "|") != "nginx-reload" {
		t.Errorf("helper calls %v", calls)
	}
}

func TestCaddySiteVPN(t *testing.T) {
	dir := t.TempDir()
	e := &Engine{SitesDir: dir, ReloadCaddy: func(context.Context) error { return nil }}
	app := store.App{ID: "web", Domain: "web.example.com", Access: AccessVPN, Allow: "10.8.0.0/24"}
	if err := e.publish(context.Background(), testLogger(), app, 35001); err != nil {
		t.Fatal(err)
	}
	conf, _ := os.ReadFile(filepath.Join(dir, "web.caddy"))
	if st, _ := os.Stat(filepath.Join(dir, "web.caddy")); st.Mode().Perm() != 0o644 {
		t.Errorf("site mode %v, the proxy could not read it", st.Mode().Perm())
	}
	if !strings.Contains(string(conf), "@outside not remote_ip 10.8.0.0/24") || !strings.Contains(string(conf), "respond @outside 403") {
		t.Errorf("caddy site:\n%s", conf)
	}
}

// A URL paused from the Proxy tab stays off: a deployment neither brings it back nor
// stops the app.
func TestPausedSiteIsNotRepublished(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	e := &Engine{Store: st, SitesDir: dir, ReloadCaddy: func(context.Context) error { return nil }}
	app := store.App{ID: "web", Domain: "web.example.com", Access: AccessPublic, Build: BuildCompose, Port: 3000}
	ctx := context.Background()

	if err := e.SetPaused(ctx, app, true); err != nil {
		t.Fatal(err)
	}
	if err := e.publish(ctx, testLogger(), app, 3000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "web.caddy")); !os.IsNotExist(err) {
		t.Fatalf("a deployment wrote the site of a paused app: %v", err)
	}

	// Turning it back on writes the snippet again, at the app's port.
	if err := e.SetPaused(ctx, app, false); err != nil {
		t.Fatal(err)
	}
	conf, err := os.ReadFile(filepath.Join(dir, "web.caddy"))
	if err != nil || !strings.Contains(string(conf), "reverse_proxy 127.0.0.1:3000") {
		t.Fatalf("resume: %v\n%s", err, conf)
	}
}

// Deleting an app takes its snippet with it, and drops its paused flag.
func TestRemoveWithdrawsTheSite(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sites := filepath.Join(root, "sites")
	if err := os.MkdirAll(sites, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// A linked folder skips the docker CLI, which a test machine may not have.
	app := store.App{ID: "blog", Dir: "/tmp/blog", Domain: "blog.example.com", Access: AccessPublic}
	if err := st.AddApp(ctx, &app); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Store: st, DataDir: filepath.Join(root, "data"), SitesDir: sites,
		ReloadCaddy: func(context.Context) error { return nil }}
	file := filepath.Join(sites, "blog.caddy")
	if err := os.WriteFile(file, []byte("# Managed by VPS Admin (app blog)\nblog.example.com {\n\treverse_proxy 127.0.0.1:8080\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPaused(ctx, app, true); err != nil {
		t.Fatal(err)
	}
	if err := e.Remove(ctx, app, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("the Caddy snippet is still there: %v", err)
	}
	if e.Paused(ctx, "blog") {
		t.Fatal("the paused flag outlived the app")
	}
}

// DeleteSite takes any file of the proxy's directory, but never one an app still owns.
func TestDeleteSite(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sites := filepath.Join(root, "sites")
	if err := os.MkdirAll(sites, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e := &Engine{Store: st, DataDir: filepath.Join(root, "data"), SitesDir: sites,
		ReloadCaddy: func(context.Context) error { return nil }}
	hand := filepath.Join(sites, "hand.caddy")
	if err := os.WriteFile(hand, []byte("hand.example.com {\n\tfile_server\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteSite(ctx, hand); err != nil {
		t.Fatalf("hand-written site: %v", err)
	}
	if _, err := os.Stat(hand); !os.IsNotExist(err) {
		t.Fatalf("the hand-written site is still there: %v", err)
	}
	if err := e.DeleteSite(ctx, "/etc/passwd"); err == nil {
		t.Fatal("a file outside the site directories was accepted")
	}

	// The site of a live app is refused: the next deploy would write it again.
	app := store.App{ID: "live", Dir: "/tmp/live", Domain: "live.example.com", Access: AccessPublic}
	if err := st.AddApp(ctx, &app); err != nil {
		t.Fatal(err)
	}
	live := filepath.Join(sites, "live.caddy")
	if err := os.WriteFile(live, []byte("live.example.com {\n\treverse_proxy 127.0.0.1:3000\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteSite(ctx, live); err == nil {
		t.Fatal("the site of a live app was deleted")
	}
	if err := e.PauseSite(ctx, live); err == nil {
		t.Fatal("the site of a live app was paused")
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatal("the app's site was touched")
	}
}

// A paused site is kept aside, so the same file can be put back exactly as it was.
func TestPauseSiteKeepsTheFile(t *testing.T) {
	root := t.TempDir()
	sites := filepath.Join(root, "sites")
	if err := os.MkdirAll(sites, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e := &Engine{DataDir: filepath.Join(root, "data"), SitesDir: sites,
		ReloadCaddy: func(context.Context) error { return nil }}
	body := "hand.example.com {\n\tfile_server\n}\n"
	path := filepath.Join(sites, "hand.caddy")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.PauseSite(ctx, path); err != nil {
		t.Fatal(err)
	}
	paused := e.PausedSites()
	if len(paused) != 1 || paused[0].File != path || paused[0].Engine != "caddy" {
		t.Fatalf("paused = %+v", paused)
	}
	if err := e.ResumeSite(ctx, path); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != body {
		t.Fatalf("after resume: %v %q", err, got)
	}
	if len(e.PausedSites()) != 0 {
		t.Fatalf("still listed as paused: %+v", e.PausedSites())
	}
}

func TestAllowList(t *testing.T) {
	if _, err := AllowList("10.8.0.0/24, nope"); err == nil {
		t.Error("bad network accepted")
	}
	if n, _ := AllowList(""); len(n) != 0 {
		t.Error("empty list should give nothing")
	}
}

// A certificate is asked for from the panel, on any nginx site; Caddy never needs it.
func TestEnableHTTPS(t *testing.T) {
	root := t.TempDir()
	sites := filepath.Join(root, "nginx")
	if err := os.MkdirAll(sites, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var calls []string
	e := &Engine{NginxDir: sites, DataDir: filepath.Join(root, "data"),
		Helper: func(_ context.Context, args ...string) (string, error) {
			calls = append(calls, strings.Join(args, " "))
			return "", nil
		}}
	path := filepath.Join(sites, "blog.conf")
	if err := os.WriteFile(path, []byte("server {\n\tlisten 80;\n\tserver_name blog.example.com;\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.EnableHTTPS(ctx, path, "Blog.Example.com"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "certbot blog.example.com" {
		t.Fatalf("calls = %v", calls)
	}
	// A paused site has no file in the directory certbot would edit.
	if err := e.PauseSite(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := e.EnableHTTPS(ctx, path, "blog.example.com"); err == nil {
		t.Fatal("a paused site accepted a certificate")
	}
	// Caddy issues its own.
	caddy := filepath.Join(root, "caddy")
	if err := os.MkdirAll(caddy, 0o755); err != nil {
		t.Fatal(err)
	}
	e.SitesDir = caddy
	cfile := filepath.Join(caddy, "web.caddy")
	if err := os.WriteFile(cfile, []byte("web.example.com {\n\tfile_server\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.EnableHTTPS(ctx, cfile, "web.example.com"); err == nil {
		t.Fatal("Caddy asked certbot")
	}
}
