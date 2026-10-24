package github

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRepoURL(t *testing.T) {
	valid := map[string]string{
		"navidrome/navidrome":                               "navidrome/navidrome",
		"https://github.com/navidrome/navidrome":            "navidrome/navidrome",
		"https://github.com/navidrome/navidrome.git":        "navidrome/navidrome",
		"https://www.github.com/navidrome/navidrome/":       "navidrome/navidrome",
		"github.com/navidrome/navidrome":                    "navidrome/navidrome",
		"git@github.com:navidrome/navidrome.git":            "navidrome/navidrome",
		"ssh://git@github.com/navidrome/navidrome.git":      "navidrome/navidrome",
		"https://github.com/navidrome/navidrome?tab=readme": "navidrome/navidrome",
	}
	for in, want := range valid {
		got, err := ParseRepoURL(in)
		if err != nil || got != want {
			t.Errorf("ParseRepoURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}

	for _, in := range []string{
		"",
		"https://gitlab.com/owner/repo",
		"https://github.com/owner",
		"https://github.com/owner/repo/tree/main",
		"https://github.com/-owner/repo",
		"not a repository",
	} {
		if got, err := ParseRepoURL(in); err == nil {
			t.Errorf("ParseRepoURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestPublicRepositoryNeedsNoToken(t *testing.T) {
	var auth []string
	checkAuth := func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			auth = append(auth, h)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/navidrome/navidrome", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(w, r)
		io.WriteString(w, `{"full_name":"navidrome/navidrome","private":false,"default_branch":"master","language":"Go","pushed_at":"2026-10-22T10:00:00Z"}`)
	})
	mux.HandleFunc("GET /repos/navidrome/navidrome/commits/master", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(w, r)
		io.WriteString(w, `{"sha":"abc123","commit":{"message":"build: release\n\nmore text"}}`)
	})
	mux.HandleFunc("GET /repos/navidrome/navidrome/branches", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(w, r)
		io.WriteString(w, `[{"name":"master"},{"name":"develop"}]`)
	})
	mux.HandleFunc("GET /repos/navidrome/navidrome/contents/", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(w, r)
		io.WriteString(w, `[{"name":"Dockerfile","type":"file"},{"name":"src","type":"dir"}]`)
	})
	mux.HandleFunc("GET /repos/navidrome/navidrome/contents/Dockerfile", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(w, r)
		io.WriteString(w, `{"content":"RlJPTSBhbHBpbmUK","encoding":"base64"}`)
	})
	mux.HandleFunc("GET /navidrome/navidrome/tar.gz/abc123", func(w http.ResponseWriter, r *http.Request) {
		checkAuth(w, r)
		io.WriteString(w, "tarball")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New()
	c.API, c.Archive, c.HTTP = srv.URL, srv.URL, srv.Client()
	ctx := context.Background()

	rp, err := c.PublicRepo(ctx, "navidrome/navidrome")
	if err != nil {
		t.Fatal(err)
	}
	if rp.FullName != "navidrome/navidrome" || rp.DefaultBranch != "master" || rp.InstallationID != 0 {
		t.Fatalf("repo = %+v", rp)
	}
	commit, err := c.Commit(ctx, 0, rp.FullName, "master")
	if err != nil || commit.SHA != "abc123" || commit.Message != "build: release" {
		t.Fatalf("commit = %+v, err = %v", commit, err)
	}
	branches, err := c.Branches(ctx, 0, rp.FullName)
	if err != nil || strings.Join(branches, ",") != "master,develop" {
		t.Fatalf("branches = %v, err = %v", branches, err)
	}
	files, err := c.Files(ctx, 0, rp.FullName, "master", "")
	if err != nil || strings.Join(files, ",") != "Dockerfile,src/" {
		t.Fatalf("files = %v, err = %v", files, err)
	}
	df, err := c.File(ctx, 0, rp.FullName, "master", "Dockerfile")
	if err != nil || !strings.HasPrefix(df, "FROM alpine") {
		t.Fatalf("Dockerfile = %q, err = %v", df, err)
	}
	tar, err := c.Tarball(ctx, 0, rp.FullName, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(tar)
	tar.Close()
	if string(body) != "tarball" {
		t.Fatalf("tarball = %q", body)
	}
	if len(auth) != 0 {
		t.Fatalf("public requests sent credentials: %v", auth)
	}
}
