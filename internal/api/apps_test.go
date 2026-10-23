package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/store"
)

func TestEnvFileSetting(t *testing.T) {
	s := alertServer(t)
	ctx := context.Background()
	if err := s.Store.AddApp(ctx, &store.App{ID: "web", Repo: "o/r", Branch: "main", Build: deploy.BuildCompose}); err != nil {
		t.Fatal(err)
	}
	call := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/api/apps/web/env", strings.NewReader(body))
		req.SetPathValue("id", "web")
		s.appSetEnv(rec, req)
		return rec
	}

	if rec := call(`{"env":[],"envFile":true,"envPath":"config/.env"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	a, err := s.Store.App(ctx, "web")
	if err != nil || !a.EnvFile || a.EnvPath != "config/.env" {
		t.Fatalf("saved = %+v, err = %v", a, err)
	}

	// A path that would write outside the app folder is refused, and nothing changes.
	if rec := call(`{"env":[],"envPath":"../etc/passwd"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("escape status = %d, want 400", rec.Code)
	}
	if a, _ = s.Store.App(ctx, "web"); a.EnvPath != "config/.env" {
		t.Fatalf("a refused path was written: %q", a.EnvPath)
	}

	// An older panel that only knows about variables leaves the file settings alone.
	if rec := call(`{"env":[]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if a, _ = s.Store.App(ctx, "web"); !a.EnvFile || a.EnvPath != "config/.env" {
		t.Fatalf("an env save wiped the file settings: %+v", a)
	}
}
