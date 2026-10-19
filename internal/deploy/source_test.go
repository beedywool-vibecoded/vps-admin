package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		files           []string
		pkg, dockerfile string
		build           string
		port            int
	}{
		{[]string{"docker-compose.yml", "Dockerfile"}, "", "", BuildCompose, 0},
		{[]string{"Dockerfile"}, "", "FROM x\nEXPOSE 8080\n", BuildDockerfile, 8080},
		{[]string{"Dockerfile"}, "", "FROM x\n", BuildDockerfile, 0},
		{[]string{"package.json"}, `{"scripts":{"start":"node s.js"}}`, "", BuildNode, 3000},
		{[]string{"package.json"}, `{"scripts":{"build":"vite build"}}`, "", BuildStatic, 80},
		{[]string{"index.html", "style.css"}, "", "", BuildStatic, 80},
		{[]string{"README.md"}, "", "", "", 0},
	}
	for _, c := range cases {
		d := Detect(c.files, c.pkg, c.dockerfile)
		if d.Build != c.build || d.Port != c.port {
			t.Errorf("%v: got %s/%d, want %s/%d", c.files, d.Build, d.Port, c.build, c.port)
		}
	}
}

func TestGeneratedDockerfile(t *testing.T) {
	if df := GeneratedDockerfile(BuildNode, []string{"pnpm-lock.yaml"}, true, Commands{}); !strings.Contains(df, "pnpm install --frozen-lockfile") {
		t.Error("lockfile not honored")
	}
	if df := GeneratedDockerfile(BuildStatic, nil, false, Commands{}); strings.Contains(df, "node") {
		t.Error("plain static site should not need node")
	}
	df := GeneratedDockerfile(BuildNode, nil, true, Commands{Install: "npm ci --omit=dev", Build: "npm run compile", Start: "node dist/server.js --port 3000"})
	for _, want := range []string{"RUN npm ci --omit=dev", "RUN npm run compile", `CMD ["sh", "-c", "node dist/server.js --port 3000"]`} {
		if !strings.Contains(df, want) {
			t.Errorf("custom command missing: %s\n%s", want, df)
		}
	}
	if df := GeneratedDockerfile(BuildStatic, nil, false, Commands{Build: "make site"}); !strings.Contains(df, "RUN make site") {
		t.Error("custom static build ignored")
	}
}

type entry struct {
	name, body, link string
	typ              byte
}

func tarball(t *testing.T, entries []entry) *bytes.Buffer {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Size: int64(len(e.body)), Linkname: e.link}
		if e.typ != tar.TypeReg {
			h.Size = 0
		}
		if e.typ == tar.TypeXGlobalHeader { // what GitHub puts first, with the commit id
			h = &tar.Header{Typeflag: e.typ, Name: e.name, PAXRecords: map[string]string{"comment": "abc"}}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(e.body))
	}
	tw.Close()
	gz.Close()
	return &buf
}

func TestExtract(t *testing.T) {
	dst := t.TempDir()
	err := Extract(tarball(t, []entry{
		{name: "pax_global_header", typ: tar.TypeXGlobalHeader},
		{name: "o-r-abc/", typ: tar.TypeDir},
		{name: "o-r-abc/index.html", body: "hi", typ: tar.TypeReg},
		{name: "o-r-abc/sub/a.txt", body: "a", typ: tar.TypeReg},
		{name: "o-r-abc/ok-link", link: "sub/a.txt", typ: tar.TypeSymlink},
		{name: "o-r-abc/bad-link", link: "/etc/passwd", typ: tar.TypeSymlink},
		{name: "o-r-abc/up-link", link: "../../x", typ: tar.TypeSymlink},
	}), dst)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "index.html")); string(b) != "hi" {
		t.Error("top folder not stripped")
	}
	if _, err := os.Lstat(filepath.Join(dst, "ok-link")); err != nil {
		t.Error("inner link dropped")
	}
	for _, l := range []string{"bad-link", "up-link"} {
		if _, err := os.Lstat(filepath.Join(dst, l)); err == nil {
			t.Errorf("%s should be skipped", l)
		}
	}
}

func TestExtractRefusesTraversal(t *testing.T) {
	err := Extract(tarball(t, []entry{{name: "o-r-abc/../../evil", body: "x", typ: tar.TypeReg}}), t.TempDir())
	if err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestSafeSubdir(t *testing.T) {
	src := t.TempDir()
	os.Mkdir(filepath.Join(src, "web"), 0o755)
	if d, err := safeSubdir(src, "web/"); err != nil || d != filepath.Join(src, "web") {
		t.Errorf("got %s, %v", d, err)
	}
	if d, err := safeSubdir(src, "../.."); err != nil || d != src {
		t.Errorf("traversal should clamp to the root, got %s, %v", d, err)
	}
	if _, err := safeSubdir(src, "missing"); err == nil {
		t.Error("missing folder accepted")
	}
}
