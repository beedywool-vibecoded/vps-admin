package deploy

// Linked folder: pushes fast-forward and rebuild, local changes stop the update,
// rollbacks reset it.
//
//	VPSADMIN_DOCKER_TEST=1 go test ./internal/deploy -run Folder -v

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/github"
	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

func TestDockerFolderUpdate(t *testing.T) {
	if os.Getenv("VPSADMIN_DOCKER_TEST") == "" {
		t.Skip("set VPSADMIN_DOCKER_TEST=1 to run against the local Docker engine")
	}
	root := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	// The bare "GitHub" repo lives inside the linked folder so the git container can reach it.
	project := fmt.Sprintf("ftest-%d", time.Now().Unix()%100000)
	folder := filepath.Join(root, project)
	work := filepath.Join(root, "work")
	os.MkdirAll(work, 0o755)
	run(work, "git", "init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(work, "site"), 0o755)
	os.WriteFile(filepath.Join(work, ".gitignore"), []byte("remote.git/\n"), 0o644)
	os.WriteFile(filepath.Join(work, "compose.yaml"), fmt.Appendf(nil, "services:\n  web:\n    build: .\n    ports: [\"127.0.0.1:%d:80\"]\n", port), 0o644)
	os.WriteFile(filepath.Join(work, "Dockerfile"), []byte("FROM nginx:1.27-alpine\nCOPY site /usr/share/nginx/html\n"), 0o644)
	commit := func(body string) string {
		os.WriteFile(filepath.Join(work, "site", "index.html"), []byte(body), 0o644)
		run(work, "git", "add", "-A")
		run(work, "git", "commit", "-q", "-m", body)
		run(work, "git", "push", "-q", filepath.Join(folder, "remote.git"), "main")
		return run(work, "git", "rev-parse", "HEAD")
	}
	run(root, "git", "clone", "-q", work, folder)
	run(folder, "git", "init", "-q", "--bare", "remote.git")
	v1 := commit("version one")
	run(folder, "git", "pull", "-q", filepath.Join(folder, "remote.git"), "main")

	head := v1
	msg := "version one"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			fmt.Fprint(w, `{"token":"t","expires_at":"2099-01-01T00:00:00Z"}`)
		case strings.Contains(r.URL.Path, "/commits/"):
			fmt.Fprintf(w, `{"sha":%q,"commit":{"message":%q}}`, head, msg)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	gh := github.New()
	gh.API = srv.URL
	if err := gh.SetCreds(&github.Creds{ID: 1, Slug: "test", PEM: string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := secret.Open(dir)
	e := &Engine{Store: st, Box: box, GitHub: gh, DataDir: dir, ReloadCaddy: func(context.Context) error { return nil }}
	e.gitRemote = func(string, string) string { return filepath.Join(folder, "remote.git") }
	e.Init()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)

	app := store.App{ID: project, Repo: "o/r", InstallationID: 1, Branch: "main", Build: BuildCompose, Keep: 5, AutoDeploy: true, Dir: folder}
	defer e.dockerOut(context.Background(), "compose", "-p", project, "down", "--rmi", "local")
	// Started by hand before linking, like an existing stack.
	if err := e.compose(ctx, io.Discard, app, folder, "up", "-d", "--build"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddApp(ctx, &app); err != nil {
		t.Fatal(err)
	}

	info, err := e.InspectFolder(ctx, folder)
	if err != nil || info.Branch != "main" || info.Head != v1 {
		t.Fatalf("inspect: %+v %v", info, err)
	}
	get := func() string {
		for range 20 {
			if res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port)); err == nil {
				b, _ := io.ReadAll(res.Body)
				res.Body.Close()
				return strings.TrimSpace(string(b))
			}
			time.Sleep(500 * time.Millisecond)
		}
		return "unreachable"
	}
	wait := func(d *store.Deployment) *store.Deployment {
		t.Helper()
		for range 360 {
			cur, _ := st.Deployment(ctx, d.ID)
			if cur.FinishedAt != nil {
				if cur.Status == "failed" {
					log, _ := st.DeploymentLog(ctx, d.ID)
					t.Logf("log of #%d:\n%s", d.ID, log)
				}
				return cur
			}
			time.Sleep(500 * time.Millisecond)
		}
		t.Fatalf("deployment %d did not finish", d.ID)
		return nil
	}
	if got := get(); got != "version one" {
		t.Fatalf("before linking: %q", got)
	}

	head, msg = commit("version two"), "version two"
	r2 := wait(must(e.Deploy(ctx, app, "push", "", "", "")))
	if r2.Status != "live" || r2.SHA != head || get() != "version two" {
		t.Fatalf("push: %+v, body %q", r2, get())
	}
	if cur := e.CurrentCommit(ctx, app); cur != head {
		t.Fatalf("folder HEAD %s, want %s", cur, head)
	}
	if out, _ := e.dockerOut(ctx, "ps", "-q", "--filter", "label=com.docker.compose.project="+project); len(strings.Fields(out)) != 1 {
		t.Fatalf("want the same single container, got %q", out)
	}

	// A local commit on the server: the update must stop, not overwrite it.
	os.WriteFile(filepath.Join(folder, "local.txt"), []byte("mine"), 0o644)
	run(folder, "git", "add", "local.txt")
	run(folder, "git", "commit", "-q", "-m", "local")
	head, msg = commit("version three"), "version three"
	r3 := wait(must(e.Deploy(ctx, app, "push", "", "", "")))
	if r3.Status != "failed" || !strings.Contains(r3.Error, "local changes") || get() != "version two" {
		t.Fatalf("diverged folder: %+v", r3)
	}

	// Rollback resets to the commit, local commit included.
	r4 := wait(must(e.Deploy(ctx, app, "rollback", v1, "version one", "")))
	if r4.Status != "live" || get() != "version one" || e.CurrentCommit(ctx, app) != v1 {
		t.Fatalf("rollback: %+v body %q", r4, get())
	}

	// Unlinking leaves the stack running.
	if err := e.Remove(ctx, app, true); err != nil {
		t.Fatal(err)
	}
	if get() != "version one" {
		t.Fatal("unlinking stopped the stack")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
