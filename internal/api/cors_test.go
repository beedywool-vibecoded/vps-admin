package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beedywool/vps-admin/internal/config"
)

func TestClientCORS(t *testing.T) {
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ClientOrigins = []string{"https://vps-admin.example.com"}
	s := &Server{Cfg: cfg}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := s.clientCORS(next)

	// Preflight from the client: answered here, token header allowed, never credentials
	r := httptest.NewRequest("OPTIONS", "/api/system", nil)
	r.Header.Set("Origin", "https://vps-admin.example.com")
	r.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "https://vps-admin.example.com" {
		t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials must never be allowed")
	}

	// A real request from the client goes through with the header
	r = httptest.NewRequest("GET", "/api/system", nil)
	r.Header.Set("Origin", "https://vps-admin.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "https://vps-admin.example.com" {
		t.Fatalf("request: %d %v", rec.Code, rec.Header())
	}

	// Another origin, or a path outside /api: no CORS header at all
	for _, c := range []struct{ origin, path string }{{"https://evil.example.com", "/api/system"}, {"https://vps-admin.example.com", "/"}} {
		r = httptest.NewRequest("GET", c.path, nil)
		r.Header.Set("Origin", c.origin)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s %s: unexpected CORS header", c.origin, c.path)
		}
	}
}
