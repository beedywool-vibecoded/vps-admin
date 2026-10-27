package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/config"
)

func TestClientIP(t *testing.T) {
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Cfg: cfg}
	cases := []struct{ remote, xff, want string }{
		{"203.0.113.9:1234", "", "203.0.113.9"},
		{"203.0.113.9:1234", "10.0.0.1", "203.0.113.9"},             // untrusted peer cannot spoof
		{"127.0.0.1:1234", "198.51.100.7", "198.51.100.7"},          // behind the local proxy
		{"127.0.0.1:1234", "6.6.6.6, 198.51.100.7", "198.51.100.7"}, // rightmost untrusted hop wins
		{"127.0.0.1:1234", "", "127.0.0.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := s.clientIP(r); got != c.want {
			t.Errorf("remote %s xff %q: got %s, want %s", c.remote, c.xff, got, c.want)
		}
	}
}

// Connecting GitHub posts a form to github.com; a stricter form-action breaks it
// (Firefox reports NS_ERROR 0x805a0061).
func TestCSPAllowsGitHubForm(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://github.com") {
		t.Fatalf("CSP blocks the GitHub App form: %s", csp)
	}
}

// The Proxy page reads the proxy config with each domain's upstream.
func TestProxySitesReadsTheConfig(t *testing.T) {
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg.CaddySitesDir = filepath.Join(root, "etc/caddy/vps-admin-apps")
	cfg.NginxSitesDir = filepath.Join(root, "etc/nginx/vps-admin-apps")
	if err := os.MkdirAll(cfg.CaddySitesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/caddy/Caddyfile"), []byte("import vps-admin-apps/*.caddy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snippet := "blog.example.com {\n\treverse_proxy 127.0.0.1:8080\n}\n"
	if err := os.WriteFile(filepath.Join(cfg.CaddySitesDir, "blog.caddy"), []byte(snippet), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{Cfg: cfg}
	rec := httptest.NewRecorder()
	s.proxySites(rec, httptest.NewRequest("GET", "/api/proxy", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var rep struct {
		Engine string
		Root   string
		Sites  []struct {
			Domains []string
			Target  string
			Managed bool
			ID      string
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Engine != "caddy" || len(rep.Sites) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	site := rep.Sites[0]
	if len(site.Domains) != 1 || site.Domains[0] != "blog.example.com" || site.Target != "127.0.0.1:8080" || !site.Managed || site.ID != "blog" {
		t.Fatalf("site = %+v", site)
	}
}

// Lists are sent as [], never null.
func TestProxySitesSendsEmptyLists(t *testing.T) {
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg.CaddySitesDir = filepath.Join(root, "caddy")
	cfg.NginxSitesDir = filepath.Join(root, "nginx")

	s := &Server{Cfg: cfg}
	rec := httptest.NewRecorder()
	s.proxySites(rec, httptest.NewRequest("GET", "/api/proxy", nil))
	body := rec.Body.String()
	if strings.Contains(body, "null") {
		t.Fatalf("body has a null list: %s", body)
	}
	if !strings.Contains(body, `"sites":[]`) || !strings.Contains(body, `"unreadable":[]`) {
		t.Fatalf("body = %s", body)
	}
}
