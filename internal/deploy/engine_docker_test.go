package deploy

// Runs real builds against the local Docker engine with a fake GitHub:
//
//	VPSADMIN_DOCKER_TEST=1 go test ./internal/deploy -run Docker -v

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/github"
	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

func sourceTar(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "o-r-sha/", Typeflag: tar.TypeDir, Mode: 0o755})
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: "o-r-sha/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestDockerDeployLifecycle(t *testing.T) {
	if os.Getenv("VPSADMIN_DOCKER_TEST") == "" {
		t.Skip("set VPSADMIN_DOCKER_TEST=1 to run against the local Docker engine")
	}
	commits := map[string]map[string]string{
		"aaaaaaaaaaaa1": {"index.html": "version one"},
		"bbbbbbbbbbbb2": {"index.html": "version two"},
		// Listens on nothing: must fail the health check and leave v2 online.
		"cccccccccccc3": {"Dockerfile": "FROM alpine:3.20\nCMD [\"sleep\", \"600\"]\n"},
	}
	head := "aaaaaaaaaaaa1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			fmt.Fprint(w, `{"token":"t","expires_at":"2099-01-01T00:00:00Z"}`)
		case strings.Contains(r.URL.Path, "/commits/"):
			fmt.Fprintf(w, `{"sha":%q,"commit":{"message":"commit %s"}}`, head, head[:7])
		case strings.Contains(r.URL.Path, "/tarball/"):
			sha := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			w.Write(sourceTar(commits[sha]))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	gh := github.New()
	gh.API = srv.URL
	if err := gh.SetCreds(&github.Creds{ID: 1, Slug: "test", PEM: pemKey}); err != nil {
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
	e.Init()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)

	app := store.App{ID: fmt.Sprintf("itest-%d", time.Now().Unix()%100000), Repo: "o/r", InstallationID: 1, Branch: "main", Build: BuildAuto, Keep: 5, AutoDeploy: true}
	if err := st.AddApp(ctx, &app); err != nil {
		t.Fatal(err)
	}
	defer e.Remove(context.Background(), app, true)

	wait := func(d *store.Deployment) *store.Deployment {
		t.Helper()
		for range 240 {
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
	get := func(port int) string {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err != nil {
			return "error: " + err.Error()
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return strings.TrimSpace(string(b))
	}
	appNow := func() store.App {
		a, _ := st.App(ctx, app.ID)
		return *a
	}

	d1, err := e.Deploy(ctx, app, "create", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r1 := wait(d1)
	if r1.Status != "live" || r1.Build != BuildStatic || get(r1.HostPort) != "version one" {
		t.Fatalf("first deploy: %+v, body %q", r1, get(r1.HostPort))
	}

	head = "bbbbbbbbbbbb2"
	d2, _ := e.Deploy(ctx, appNow(), "push", "", "", "")
	r2 := wait(d2)
	if r2.Status != "live" || get(r2.HostPort) != "version two" {
		t.Fatalf("second deploy: %+v", r2)
	}
	if old, _ := st.Deployment(ctx, d1.ID); old.Status != "superseded" {
		t.Errorf("first deployment is %s, want superseded", old.Status)
	}
	if out, _ := e.dockerOut(ctx, "ps", "-q", "--filter", "name="+r1.Container); out != "" {
		t.Error("previous container still running")
	}

	// A broken build must not take the app down.
	head = "cccccccccccc3"
	a := appNow()
	a.Build = BuildAuto
	d3, _ := e.Deploy(ctx, a, "push", "", "", "")
	r3 := wait(d3)
	if r3.Status != "failed" {
		t.Fatalf("broken deploy: %+v", r3)
	}
	if live, _ := st.LiveDeployment(ctx, app.ID); live.ID != d2.ID || get(live.HostPort) != "version two" {
		t.Fatal("the working version did not survive a failed deploy")
	}

	// Rollback reuses the first image.
	d4, _ := e.Deploy(ctx, appNow(), "rollback", r1.SHA, r1.Message, r1.Image)
	r4 := wait(d4)
	if r4.Status != "live" || get(r4.HostPort) != "version one" {
		t.Fatalf("rollback: %+v", r4)
	}
	log, _ := st.DeploymentLog(ctx, d4.ID)
	if strings.Contains(log, "Building image") {
		t.Error("rollback rebuilt the image")
	}

	if err := e.Remove(ctx, appNow(), true); err != nil {
		t.Fatal(err)
	}
	if out, _ := e.dockerOut(ctx, "ps", "-aq", "--filter", "label=vpsadmin.app="+app.ID); out != "" {
		t.Error("containers left after remove")
	}
	if out, _ := e.dockerOut(ctx, "images", "-q", "vpsadmin/"+app.ID); out != "" {
		t.Error("images left after remove")
	}
}
