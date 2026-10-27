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

// fakeGitHub serves one organization install with one repository.
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

// fakePublicGitHub serves the unauthenticated repository endpoint, without App credentials.
func fakePublicGitHub(t *testing.T, repo string) *github.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/"+repo, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("public repository request sent credentials")
		}
		w.Write([]byte(`{"full_name":"` + repo + `","private":false,"default_branch":"master","language":"Go","pushed_at":"2026-10-22T10:00:00Z"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := github.New()
	c.API, c.HTTP = srv.URL, srv.Client()
	return c
}

func TestGitHubPublicRepo(t *testing.T) {
	s := alertServer(t)
	s.GitHub = fakePublicGitHub(t, "navidrome/navidrome")
	rec := httptest.NewRecorder()
	s.githubPublicRepo(rec, httptest.NewRequest("POST", "/api/github/public", strings.NewReader(`{"url":"https://github.com/navidrome/navidrome"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	repo, _ := decode(t, rec)["repo"].(map[string]any)
	if repo["fullName"] != "navidrome/navidrome" || repo["defaultBranch"] != "master" {
		t.Fatalf("repo = %v", repo)
	}

	rec = httptest.NewRecorder()
	s.githubPublicRepo(rec, httptest.NewRequest("POST", "/api/github/public", strings.NewReader(`{"url":"not a repository"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad URL status = %d, want 400", rec.Code)
	}
}

func TestAppCreateFromPublicURL(t *testing.T) {
	s := alertServer(t)
	s.GitHub = fakePublicGitHub(t, "navidrome/navidrome")
	rec := httptest.NewRecorder()
	body := `{"id":"navidrome","repo":"https://github.com/navidrome/navidrome","branch":"master","rootDir":"","build":"auto","port":0,"domain":"","autoDeploy":true,"keep":5,"memoryMb":0,"access":"local","external":true}`
	s.appCreate(rec, httptest.NewRequest("POST", "/api/apps", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	a, err := s.Store.App(context.Background(), "navidrome")
	if err != nil {
		t.Fatal(err)
	}
	if a.Repo != "navidrome/navidrome" || a.InstallationID != 0 {
		t.Fatalf("stored source = %s on %d", a.Repo, a.InstallationID)
	}
	// Public URLs have no webhook, so auto deploy must be off.
	if a.AutoDeploy {
		t.Fatal("a public repository was stored with automatic deploys on")
	}

	// The settings form cannot turn them back on either.
	update := `{"branch":"master","rootDir":"","build":"auto","port":0,"domain":"","autoDeploy":true,"keep":5,"memoryMb":0,"access":"local"}`
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/apps/navidrome", strings.NewReader(update))
	req.SetPathValue("id", "navidrome")
	s.appUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", rec.Code, rec.Body.String())
	}
	if a, _ = s.Store.App(context.Background(), "navidrome"); a.AutoDeploy {
		t.Fatal("an update turned automatic deploys on for a public repository")
	}
}

func TestAppCreateRefusesPrivateURL(t *testing.T) {
	s := alertServer(t)
	s.GitHub = fakePublicGitHub(t, "navidrome/navidrome")
	rec := httptest.NewRecorder()
	body := `{"id":"private","repo":"https://github.com/acme/private","branch":"main","rootDir":"","build":"auto","port":0,"domain":"","autoDeploy":false,"keep":5,"memoryMb":0,"access":"local","external":true}`
	s.appCreate(rec, httptest.NewRequest("POST", "/api/apps", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := s.Store.App(context.Background(), "private"); err == nil {
		t.Fatal("the app was created from a private URL")
	}
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

	// Paths escaping the app folder are refused.
	if rec := call(`{"env":[],"envPath":"../etc/passwd"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("escape status = %d, want 400", rec.Code)
	}
	if a, _ = s.Store.App(ctx, "web"); a.EnvPath != "config/.env" {
		t.Fatalf("a refused path was written: %q", a.EnvPath)
	}

	// Older panels that omit the file settings leave them alone.
	if rec := call(`{"env":[]}`); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if a, _ = s.Store.App(ctx, "web"); !a.EnvFile || a.EnvPath != "config/.env" {
		t.Fatalf("an env save wiped the file settings: %+v", a)
	}
}
