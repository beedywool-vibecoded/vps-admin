package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/github"
	"github.com/beedywool/vps-admin/internal/store"
)

// fakeGitHub answers the App endpoints the panel reads: one organization install and
// one repository in it.
func fakeGitHub(t *testing.T, repo string) *github.Client {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /app/installations", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":7,"html_url":"https://github.com/organizations/acme","account":{"login":"acme","type":"Organization"}}]`))
	})
	mux.HandleFunc("POST /app/installations/7/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		w.Write([]byte(`{"token":"test-token","expires_at":"` + expires + `"}`))
	})
	mux.HandleFunc("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"repositories":[{"full_name":"` + repo + `","private":true,"default_branch":"main","language":"Go","pushed_at":"2026-10-20T10:00:00Z"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := github.New()
	c.API, c.HTTP = srv.URL, srv.Client()
	if err := c.SetCreds(&github.Creds{ID: 1, PEM: string(pemKey)}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAppUpdateChangesSource(t *testing.T) {
	s := alertServer(t)
	s.GitHub = fakeGitHub(t, "acme/site")
	ctx := context.Background()
	if err := s.Store.AddApp(ctx, &store.App{ID: "web", Repo: "alice/site", InstallationID: 3,
		Branch: "main", Build: deploy.BuildAuto, Keep: 5, Access: deploy.AccessPublic}); err != nil {
		t.Fatal(err)
	}
	update := func(repo string) *httptest.ResponseRecorder {
		body := `{"repo":"` + repo + `","branch":"main","rootDir":"","build":"auto","port":0,"domain":"","autoDeploy":true,"keep":5,"memoryMb":0,"access":"public"}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/api/apps/web", strings.NewReader(body))
		req.SetPathValue("id", "web")
		s.appUpdate(rec, req)
		return rec
	}

	// The repository moved from a user to an organization: its installation follows.
	if rec := update("acme/site"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	a, err := s.Store.App(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	if a.Repo != "acme/site" || a.InstallationID != 7 {
		t.Fatalf("stored source = %s on %d", a.Repo, a.InstallationID)
	}

	// A repository the App cannot read is refused and the source stays as it was.
	if rec := update("alice/other"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unreadable source status = %d, want 400", rec.Code)
	}
	if a, err = s.Store.App(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if a.Repo != "acme/site" || a.InstallationID != 7 {
		t.Fatalf("a refused source was saved: %s on %d", a.Repo, a.InstallationID)
	}
}

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
