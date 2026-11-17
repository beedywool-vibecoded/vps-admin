package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The phone client works from any origin with a device token, and nothing else crosses origins.
func TestClientCORS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := clientCORS(next)
	const origin = "https://vps-admin.example.com"
	do := func(method, path string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Origin", origin)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	// Preflight for a token request: answered here, never with credentials
	rec := do("OPTIONS", "/api/system", map[string]string{"Access-Control-Request-Method": "GET", "Access-Control-Request-Headers": "authorization"})
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Fatalf("preflight: %d %v", rec.Code, rec.Header())
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials must never be allowed")
	}
	// Pairing has no token yet: its code is the secret
	rec = do("OPTIONS", "/api/auth/pair", map[string]string{"Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "content-type"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("pair preflight: %d", rec.Code)
	}
	// A token request is readable
	rec = do("GET", "/api/system", map[string]string{"Authorization": "Bearer vpsa_x"})
	if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != origin {
		t.Fatalf("token request: %d %v", rec.Code, rec.Header())
	}

	// Without a token, or outside /api: no CORS at all
	for _, c := range []struct {
		method, path string
		hdr          map[string]string
	}{
		{"GET", "/api/health", nil},
		{"GET", "/api/system", nil},
		{"OPTIONS", "/api/system", map[string]string{"Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "content-type"}},
		{"GET", "/", map[string]string{"Authorization": "Bearer vpsa_x"}},
	} {
		if rec := do(c.method, c.path, c.hdr); rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s %s: unexpected CORS header", c.method, c.path)
		}
	}
}
