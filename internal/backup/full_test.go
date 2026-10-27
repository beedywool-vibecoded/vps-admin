package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beedywool/vps-admin/internal/docker"
)

func writeTestFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestFullBackupCarriesAppsBackgroundAndVolumes runs the whole path: collect, archive,
// restore and apply, with the volume tar/unpack replaced by stand-ins.
func TestFullBackupCarriesAppsBackgroundAndVolumes(t *testing.T) {
	dir := t.TempDir()
	drive := fakeRclone(t)
	ctx := context.Background()
	m, st := newTestManager(t, dir)
	defer st.Close()

	writeTestFile(t, filepath.Join(dir, "background"), "wallpaper")
	writeTestFile(t, filepath.Join(dir, "apps", "web", "src", "compose.yaml"), "services:\n  web:\n    image: nginx\n")
	writeTestFile(t, filepath.Join(dir, "apps", "web", "src", ".env"), "SECRET=1\n")
	writeTestFile(t, filepath.Join(dir, "apps", "web", "src", ".git", "config"), "repository")
	writeTestFile(t, filepath.Join(dir, "apps", "web", "node_modules", "x", "index.js"), "module")

	var imported string
	m.Volumes = func(context.Context) ([]docker.VolumeUse, error) {
		return []docker.VolumeUse{{Name: "vpsadmin-web_data"}}, nil
	}
	m.exportVolumeFn = func(_ context.Context, name, dest string) error {
		return os.WriteFile(dest, []byte("tar of "+name), 0o600)
	}
	m.importVolumeFn = func(_ context.Context, name, src string) error {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		imported = name + ":" + string(b)
		return nil
	}
	name, size, err := m.run(ctx, fullID)
	if err != nil {
		t.Fatal(err)
	}
	if size < 1000 {
		t.Fatalf("the archive looks empty (%d bytes)", size)
	}

	// Open the archive itself: the files must be there, the regenerable ones not.
	payload := filepath.Join(dir, "payload")
	if err := os.MkdirAll(payload, 0o700); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(drive, DefaultPath, name)
	h, names, err := Extract(local, "a long enough passphrase", payload)
	if err != nil {
		t.Fatal(err)
	}
	if h.Kind != fullID {
		t.Fatalf("header kind = %q", h.Kind)
	}
	joined := strings.Join(names, "\n")
	for _, want := range []string{"background", "apps/web/src/compose.yaml", "apps/web/src/.env", "volumes/vpsadmin-web_data.tar"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the archive is missing %q:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{".git/config", "node_modules"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("the archive should skip %q:\n%s", unwanted, joined)
		}
	}
	if b, err := os.ReadFile(filepath.Join(payload, "volumes", "vpsadmin-web_data.tar")); err != nil || string(b) != "tar of vpsadmin-web_data" {
		t.Fatalf("the volume entry came back as %q, %v", b, err)
	}

	// Restore stages the files; applying swaps them in and leaves volumes for later.
	if _, err := m.Restore(ctx, name, "a long enough passphrase"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"background", "apps/web/src/compose.yaml", "volumes/vpsadmin-web_data.tar"} {
		if _, err := os.Stat(filepath.Join(dir, "restore", filepath.FromSlash(p))); err != nil {
			t.Fatalf("restore/%s was not staged: %v", p, err)
		}
	}
	os.RemoveAll(filepath.Join(dir, "apps", "web"))
	writeTestFile(t, filepath.Join(dir, "background"), "old wallpaper")
	st.Close()
	if err := ApplyPending(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "background")); string(b) != "wallpaper" {
		t.Fatalf("the wallpaper was not restored: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "apps", "web", "src", "compose.yaml")); !strings.Contains(string(b), "nginx") {
		t.Fatalf("the app sources were not restored: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "restore", "volumes", "vpsadmin-web_data.tar")); err != nil {
		t.Fatalf("the volumes must wait for the agent to start: %v", err)
	}

	// Volume list and unpack are stubbed; only the pause step is skipped.
	if err := m.ApplyStagedVolumes(ctx); err != nil {
		t.Fatal(err)
	}
	if imported != "vpsadmin-web_data:tar of vpsadmin-web_data" {
		t.Fatalf("the volume was not imported: %q", imported)
	}
	if _, err := os.Stat(filepath.Join(dir, "restore")); !os.IsNotExist(err) {
		t.Fatal("the staging folder must be gone after the volumes are applied")
	}
}

func TestFullBackupSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	m, st := newTestManager(t, dir)
	defer st.Close()
	writeTestFile(t, filepath.Join(dir, "apps", "web", "src", "real.txt"), "real")
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "apps", "web", "src", "link")); err != nil {
		t.Skip("symlinks are not available")
	}
	entries, err := m.appEntries()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name, "/link") {
			t.Fatalf("a symlink must not be archived: %+v", e)
		}
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
}
