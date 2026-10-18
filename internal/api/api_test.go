package api

import (
	"net/http"
	"net/http/httptest"
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
