package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.2.4", "v1.2.3", true},
		{"v1.10.0", "v1.9.9", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.2.4", false},
		{"v1.2.0", "v1.2.0-rc.1", true},
		{"v1.2.0-rc.1", "v1.2.0", false},
		{"v1.2.0-rc.10", "v1.2.0-rc.9", true},
		{"v0.0.1", "dev", true},
		{"dev", "v0.0.1", false},
		{"garbage", "v1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestParseRepo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"beedywool-vibecoded/vps-admin", "beedywool-vibecoded/vps-admin"},
		{"https://github.com/beedywool-vibecoded/vps-admin", "beedywool-vibecoded/vps-admin"},
		{"https://github.com/beedywool-vibecoded/vps-admin/", "beedywool-vibecoded/vps-admin"},
		{"http://www.github.com/o/r.git", "o/r"},
		{"github.com/o/r/releases", "o/r"},
		{"  https://github.com/o/r.git/  ", "o/r"},
		{"https://github.com/o/r?tab=readme#install", "o/r"},
	}
	for _, c := range cases {
		got, err := ParseRepo(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseRepo(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	bad := []string{"", "owner", "https://gitlab.com/o/r", "o//r", "../r", "owner with space/r", "-o/r"}
	for _, in := range bad {
		if got, err := ParseRepo(in); err == nil {
			t.Errorf("ParseRepo(%q) accepted %q", in, got)
		}
	}
}

// The source saved in the panel is the repository the next check reads.
func TestSourceChange(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	u := &Updater{Repo: "o/r", DataDir: dir, Binary: filepath.Join(dir, "vps-admin"), HTTP: srv.Client(), API: srv.URL}
	u.status.Available = true
	u.status.Latest = &Release{Tag: "v1.2.3"}

	u.SetSource("fork/r")
	if st := u.Status(); st.Available || st.Latest != nil {
		t.Fatal("a release found on the previous source was kept")
	}
	if _, err := u.latest(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if asked != "/repos/fork/r/releases" {
		t.Fatalf("checked %q, want the new source", asked)
	}
}

// A stale token gets 403 on a new public repo; the request is retried without it.
func TestCheckRetriesWithoutAStaleToken(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
			return
		}
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	u := &Updater{Repo: "o/r", DataDir: t.TempDir(), HTTP: srv.Client(), API: srv.URL, Token: "stale"}
	if _, err := u.latest(context.Background(), false); err != nil {
		t.Fatalf("a public repository must still be read with a stale token: %v", err)
	}
	if len(seen) != 2 || seen[0] == "" || seen[1] != "" {
		t.Fatalf("requests carried %q, want the token then no token", seen)
	}
}

func TestCheckKeepsTheTokenForAPrivateRepository(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		w.Write([]byte("[]"))
	}))
	defer srv.Close()
	u := &Updater{Repo: "o/r", DataDir: t.TempDir(), HTTP: srv.Client(), API: srv.URL, Token: "good"}
	if _, err := u.latest(context.Background(), false); err != nil {
		t.Fatalf("a valid token must be enough: %v", err)
	}
}

func TestCheckReportsWhatGitHubSaid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"API rate limit exceeded for 192.0.2.1"}`))
	}))
	defer srv.Close()
	u := &Updater{Repo: "o/r", DataDir: t.TempDir(), HTTP: srv.Client(), API: srv.URL}
	_, err := u.latest(context.Background(), false)
	if err == nil {
		t.Fatal("a 403 must be reported")
	}
	for _, want := range []string{"403", "API rate limit exceeded", "rate limit for this server"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestVerifySums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	saved := PublicKey
	PublicKey = base64.StdEncoding.EncodeToString(pub)
	defer func() { PublicKey = saved }()

	sums := []byte("aa11  vps-admin_linux_arm64\nbb22  vps-admin_linux_amd64\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)) + "\n")

	got, err := VerifySums(sums, sig, "vps-admin_linux_amd64")
	if err != nil || len(got) != 2 || got[0] != 0xbb {
		t.Fatalf("valid signature: got %x, %v", got, err)
	}
	tampered := append([]byte(nil), sums...)
	tampered[0] = 'f'
	if _, err := VerifySums(tampered, sig, "vps-admin_linux_amd64"); !errors.Is(err, ErrSignature) {
		t.Fatalf("tampered file accepted: %v", err)
	}
	if _, err := VerifySums(sums, sig, "vps-admin_linux_riscv64"); err == nil {
		t.Fatal("missing asset accepted")
	}
}

// fakeGitHub serves one release whose binary is a shell script printing tag.
func fakeGitHub(t *testing.T, tag string, priv ed25519.PrivateKey, corrupt bool) *httptest.Server {
	t.Helper()
	bin := []byte("#!/bin/sh\necho " + tag + "\n")
	sum := sha256.Sum256(bin)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + AssetName() + "\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums))
	if corrupt {
		bin = append(bin, '#')
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/releases":
			fmt.Fprintf(w, `[{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"%s/bin"},{"name":"SHA256SUMS","browser_download_url":"%s/sums"},{"name":"SHA256SUMS.sig","browser_download_url":"%s/sig"}]},{"tag_name":"v0.0.1","assets":[]}]`,
				tag, AssetName(), srv.URL, srv.URL, srv.URL)
		case "/bin":
			w.Write(bin)
		case "/sums":
			w.Write(sums)
		case "/sig":
			w.Write([]byte(sig))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestUpdater(t *testing.T, srv *httptest.Server) *Updater {
	dir := t.TempDir()
	binary := filepath.Join(dir, "vps-admin")
	os.WriteFile(binary, []byte("old"), 0o755)
	return &Updater{Repo: "o/r", DataDir: dir, Binary: binary, HTTP: srv.Client(), API: srv.URL,
		status: Status{Current: "v1.0.0"}}
}

func useKey(t *testing.T) ed25519.PrivateKey {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	saved := PublicKey
	PublicKey = base64.StdEncoding.EncodeToString(pub)
	t.Cleanup(func() { PublicKey = saved })
	return priv
}

func TestApply(t *testing.T) {
	priv := useKey(t)
	u := newTestUpdater(t, fakeGitHub(t, "v1.1.0", priv, false))
	rel, err := u.latest(context.Background(), false)
	if err != nil || rel.Tag != "v1.1.0" {
		t.Fatalf("latest: %+v, %v", rel, err)
	}
	if err := u.Apply(context.Background(), rel); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(u.Binary + ".prev"); string(b) != "old" {
		t.Error("previous binary not kept")
	}
	if out, _ := exec.Command(u.Binary, "version").Output(); strings.TrimSpace(string(out)) != "v1.1.0" {
		t.Errorf("new binary not installed: %q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(u.DataDir, targetFile)); strings.TrimSpace(string(b)) != "v1.1.0" {
		t.Errorf("target file: %q", b)
	}
	if _, err := os.Stat(filepath.Join(u.DataDir, pendingFile)); err != nil {
		t.Error("pending marker missing")
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	priv := useKey(t)
	u := newTestUpdater(t, fakeGitHub(t, "v1.1.0", priv, true))
	rel, _ := u.latest(context.Background(), false)
	if err := u.Apply(context.Background(), rel); !errors.Is(err, ErrChecksum) {
		t.Fatalf("got %v, want ErrChecksum", err)
	}
	if b, _ := os.ReadFile(u.Binary); string(b) != "old" {
		t.Error("binary replaced despite a bad checksum")
	}
	if _, err := os.Stat(u.Binary + ".new"); err == nil {
		t.Error("partial download left behind")
	}
}

func TestApplyRejectsForeignKey(t *testing.T) {
	useKey(t)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	u := newTestUpdater(t, fakeGitHub(t, "v1.1.0", other, false))
	rel, _ := u.latest(context.Background(), false)
	if err := u.Apply(context.Background(), rel); !errors.Is(err, ErrSignature) {
		t.Fatalf("got %v, want ErrSignature", err)
	}
}

// With a token, assets come from the API URLs with the token attached.
func TestPrivateRepoUsesAPIAssets(t *testing.T) {
	priv := useKey(t)
	bin := []byte("#!/bin/sh\necho v1.1.0\n")
	sum := sha256.Sum256(bin)
	sums := []byte(hex.EncodeToString(sum[:]) + "  " + AssetName() + "\n")
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.NotFound(w, r) // GitHub hides private repos from anonymous requests
			return
		}
		if strings.Contains(r.URL.Path, "/releases/assets/") && r.Header.Get("Accept") != "application/octet-stream" {
			t.Errorf("asset %s fetched without octet-stream", r.URL.Path)
		}
		switch r.URL.Path {
		case "/repos/o/r/releases":
			fmt.Fprintf(w, `[{"tag_name":"v1.1.0","assets":[{"name":%q,"url":"%[2]s/repos/o/r/releases/assets/1","browser_download_url":"x"},{"name":"SHA256SUMS","url":"%[2]s/repos/o/r/releases/assets/2","browser_download_url":"x"},{"name":"SHA256SUMS.sig","url":"%[2]s/repos/o/r/releases/assets/3","browser_download_url":"x"}]}]`, AssetName(), srv.URL)
		case "/repos/o/r/releases/assets/1":
			w.Write(bin)
		case "/repos/o/r/releases/assets/2":
			w.Write(sums)
		case "/repos/o/r/releases/assets/3":
			w.Write([]byte(sig))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u := newTestUpdater(t, srv)
	if _, err := u.latest(context.Background(), false); err == nil {
		t.Fatal("anonymous request should fail on a private repo")
	}
	u.Token = "secret"
	rel, err := u.latest(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Apply(context.Background(), rel); err != nil {
		t.Fatal(err)
	}
}
