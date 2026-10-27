package assistant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A provider that fails or doesn't answer must not cost the admin the key; only an auth
// error refuses it.
func TestKeyIsSavedWhenTheProviderFails(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(failing.Close)
	e := testEngine(t)
	e.GeminiURL = failing.URL
	ctx := context.Background()
	if err := e.SetKey(ctx, ProviderGemini, "AIza-example-key"); err != nil {
		t.Fatalf("a 502 from Google must not reject the key: %v", err)
	}
	if got := e.keyFor(ctx, ProviderGemini); got != "AIza-example-key" {
		t.Fatalf("the key was not saved: %q", got)
	}

	// Google answers a wrong key with 400 and this body, not 401.
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`[{"error":{"code":400,"message":"Please pass a valid API key","status":"INVALID_ARGUMENT"}}]`))
	}))
	t.Cleanup(rejecting.Close)
	e2 := testEngine(t)
	e2.GeminiURL = rejecting.URL
	if err := e2.SetKey(ctx, ProviderGemini, "AIza-bad-key"); err == nil {
		t.Fatal("a key Google refuses must be rejected")
	}
	if got := e2.keyFor(ctx, ProviderGemini); got != "" {
		t.Fatalf("a refused key must not be stored: %q", got)
	}
}

// The check is capped so a reverse proxy doesn't time out first.
func TestKeyCheckDoesNotHang(t *testing.T) {
	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	// Cleanups run LIFO: release the handler before closing its server.
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(block) })
	e := testEngine(t)
	e.GeminiURL = slow.URL
	ctx := context.Background()
	if err := e.SetKey(ctx, ProviderGemini, "AIza-example-key"); err != nil {
		t.Fatalf("a slow provider must not reject the key: %v", err)
	}
}
