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
	if !strings.Contains(string(conf), "@outside not remote_ip 10.8.0.0/24") || !strings.Contains(string(conf), "respond @outside 403") {
		t.Errorf("caddy site:\n%s", conf)
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
