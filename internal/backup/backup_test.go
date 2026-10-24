package backup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/store"
)

const testPass = "correct horse battery"

func TestArchiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "vps-admin.db")
	body := strings.Repeat("database ", 5000)
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, ArchiveName(settingsID, time.Now()))
	h := Header{Kind: settingsID, Created: time.Now().UTC(), Panel: "9.9.9", Host: "box", PublicURL: "https://panel.example"}
	err := Write(dest, testPass, h, []Entry{
		{Name: "vps-admin.db", Path: src},
		{Name: "secret.key", Data: []byte("0123456789abcdef0123456789abcdef")},
	})
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o700)
	got, names, err := Extract(dest, testPass, out)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != settingsID || got.Host != "box" || got.Panel != "9.9.9" {
		t.Fatalf("header came back as %+v", got)
	}
	if len(names) != 2 {
		t.Fatalf("want 2 entries, got %v", names)
	}
	b, err := os.ReadFile(filepath.Join(out, "vps-admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != body {
		t.Fatal("the database entry did not survive the round trip")
	}
	if b, _ := os.ReadFile(filepath.Join(out, "secret.key")); len(b) != 32 {
		t.Fatalf("secret.key came back as %q", b)
	}
}

func TestArchiveRefusesAWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "a.vpb")
	if err := Write(dest, testPass, Header{Kind: settingsID, Created: time.Now()}, []Entry{{Name: "secret.key", Data: []byte("x")}}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o700)
	_, _, err := Extract(dest, "not the passphrase", out)
	if !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("a wrong passphrase must be reported as such, got %v", err)
	}
}

func TestArchiveRefusesATamperedChunk(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "a.vpb")
	if err := Write(dest, testPass, Header{Kind: settingsID, Created: time.Now()}, []Entry{{Name: "secret.key", Data: []byte("x")}}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 0xff
	if err := os.WriteFile(dest, b, 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	os.Mkdir(out, 0o700)
	if _, _, err := Extract(dest, testPass, out); err == nil {
		t.Fatal("a damaged archive must be refused")
	}
}

func TestArchiveRefusesUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../etc/passwd", "/etc/passwd", "a/../../b", "a//b", "a/./b", "a\\b"} {
		if err := Write(filepath.Join(dir, "a.vpb"), testPass, Header{Kind: settingsID}, []Entry{{Name: name, Data: []byte("x")}}); err == nil {
			t.Errorf("entry %q should have been refused", name)
		}
	}
}

func TestNormalizeToken(t *testing.T) {
	raw := `Paste the following into your remote machine --->
{"access_token":"ya29.x","token_type":"Bearer","refresh_token":"1//refresh","expiry":"2026-10-24T15:00:00Z"}
<---End paste`
	got, err := NormalizeToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "1//refresh") || strings.Contains(got, "\n") {
		t.Fatalf("the token was not normalized: %q", got)
	}
	if _, err := NormalizeToken(`{"access_token":"x"}`); err == nil {
		t.Fatal("a token without refresh_token must be refused")
	}
	if _, err := NormalizeToken("not json"); err == nil {
		t.Fatal("garbage must be refused")
	}
	if _, err := NormalizeToken("  "); !errors.Is(err, errNoToken) {
		t.Fatalf("an empty value must be reported as clearing the token, got %v", err)
	}
}

func TestSnapshotIsReadable(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SetSetting(ctx, "appearance.opacity", "0.5"); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "snapshot.db")
	if err := st.Snapshot(ctx, dest); err != nil {
		t.Fatal(err)
	}
	if err := validSnapshot(dest); err != nil {
		t.Fatal(err)
	}
}

func TestApplyPendingSwapsTheDatabaseAndTheKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "vps-admin.db"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.key"), []byte("old-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(dir, "restore")
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "vps-admin.db"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "secret.key"), []byte("new-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPending(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "vps-admin.db")); string(b) != "new" {
		t.Fatalf("the database was not replaced: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "secret.key")); string(b) != "new-key" {
		t.Fatalf("the key was not replaced: %q", b)
	}
	if _, err := os.Stat(stage); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the staging folder must be gone")
	}
	entries, err := filepath.Glob(filepath.Join(dir, "*.before-restore-*"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("the previous files must be kept, got %v (%v)", entries, err)
	}
}

func TestApplyPendingDoesNothingWithoutAStage(t *testing.T) {
	dir := t.TempDir()
	if err := ApplyPending(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vps-admin.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("nothing should have been created")
	}
}

func TestValidSnapshotRefusesSomethingElse(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "not-a-db")
	os.WriteFile(p, []byte("hello, this is not sqlite at all"), 0o600)
	if err := validSnapshot(p); err == nil {
		t.Fatal("a plain file must be refused")
	}
	if err := validSnapshot(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file must be refused")
	}
}

func TestArchiveNameSortsAndMatchesTheRestoreFilter(t *testing.T) {
	a := ArchiveName(settingsID, time.Date(2026, 10, 24, 13, 5, 0, 0, time.UTC))
	b := ArchiveName(settingsID, time.Date(2026, 10, 25, 13, 5, 0, 0, time.UTC))
	if !archiveFile.MatchString(a) || !archiveFile.MatchString(b) {
		t.Fatalf("the names the writer makes must pass the restore filter: %s %s", a, b)
	}
	if a >= b {
		t.Fatalf("%s should sort before %s", a, b)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	os.WriteFile(src, []byte("payload"), 0o600)
	dst := filepath.Join(dir, "dst")
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	if string(b) != "payload" {
		t.Fatalf("copied %q", b)
	}
}
