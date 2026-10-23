package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/store"
)

func TestWriteEnvFile(t *testing.T) {
	root := t.TempDir()
	e := &Engine{}
	app := store.App{ID: "web", EnvFile: true, EnvPath: "config/app.env"}
	rel, err := e.writeEnvFile(root, app, []string{
		"PLAIN=simple",
		"SPACED=hello world",
		"URL=postgres://user:p@ss$word@db:5432/app",
		"HASH=a # not a comment",
		"EMPTY=",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rel != "config/app.env" {
		t.Fatalf("rel = %q", rel)
	}
	path := filepath.Join(root, rel)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, the file holds the secrets", st.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	want := strings.Join([]string{
		`PLAIN="simple"`,
		`SPACED="hello world"`,
		// $ is escaped: compose would otherwise read it as a variable.
		`URL="postgres://user:p@ss$$word@db:5432/app"`,
		`HASH="a # not a comment"`,
		"EMPTY=",
		"",
	}, "\n")
	if string(b) != want {
		t.Errorf("content:\n%s\nwant:\n%s", b, want)
	}
}

func TestWriteEnvFileDefaultsToDotEnv(t *testing.T) {
	root := t.TempDir()
	e := &Engine{}
	rel, err := e.writeEnvFile(root, store.App{ID: "web", EnvFile: true}, []string{"A=1"})
	if err != nil || rel != ".env" {
		t.Fatalf("rel = %q, err = %v", rel, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".env")); err != nil {
		t.Fatal(err)
	}
}

func TestEnvPathStaysInsideTheFolder(t *testing.T) {
	for _, ok := range []string{"", ".env", "config/app.env", "a/b/c.env"} {
		if !ValidEnvPath(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"../x", "/etc/passwd", "a/../../x", "dir/", "..", ".", `a\b`} {
		if ValidEnvPath(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
	// The writer refuses the same paths, even if a stored app bypassed the API.
	root := t.TempDir()
	e := &Engine{}
	if _, err := e.writeEnvFile(root, store.App{ID: "web", EnvFile: true, EnvPath: "../escape"}, []string{"A=1"}); err == nil {
		t.Fatal("wrote outside the app folder")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape")); err == nil {
		t.Fatal("a file landed outside the app folder")
	}
}
