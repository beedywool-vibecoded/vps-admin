package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/buildinfo"
	"github.com/beedywool/vps-admin/internal/update"
)

func TestUpdateSourceSetting(t *testing.T) {
	s := alertServer(t)
	up, err := update.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Updater = up
	ctx := context.Background()

	call := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.updateSettings(rec, httptest.NewRequest("PUT", "/api/settings/update", strings.NewReader(body)))
		return rec
	}

	// A link copied from the browser is accepted and normalized.
	rec := call(`{"repo":"https://github.com/beedywool-vibecoded/vps-admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := decode(t, rec)
	if got["repo"] != "beedywool-vibecoded/vps-admin" || got["defaultRepo"] != buildinfo.Repo {
		t.Fatalf("view = %v", got)
	}
	if up.Source() != "beedywool-vibecoded/vps-admin" {
		t.Fatalf("checks still read %q", up.Source())
	}
	if v := s.Store.Setting(ctx, "update.repo", ""); v != "beedywool-vibecoded/vps-admin" {
		t.Fatalf("stored source = %q", v)
	}

	// Non-GitHub URLs are refused.
	if rec := call(`{"repo":"https://gitlab.com/o/r"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid source status = %d, want 400", rec.Code)
	}
	if up.Source() != "beedywool-vibecoded/vps-admin" {
		t.Fatalf("a refused source was applied: %q", up.Source())
	}

	// Empty resets to the build's default repository.
	rec = call(`{"repo":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := decode(t, rec); got["repo"] != buildinfo.Repo {
		t.Fatalf("reset source = %v", got["repo"])
	}
	if v := s.Store.Setting(ctx, "update.repo", "unset"); v != "unset" {
		t.Fatalf("reset left %q stored", v)
	}
	if up.Source() != buildinfo.Repo {
		t.Fatalf("checks still read %q after reset", up.Source())
	}

	// A new process picks up the source saved in the panel.
	if rec := call(`{"repo":"fork/vps-admin"}`); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	fresh, err := update.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Updater = fresh
	s.LoadUpdateSource(ctx)
	if fresh.Source() != "fork/vps-admin" {
		t.Fatalf("startup source = %q", fresh.Source())
	}
}
