package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

// fakeRclone implements copyto, lsf, deletefile and lsd on a local folder.
func fakeRclone(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	drive := filepath.Join(bin, "drive")
	if err := os.MkdirAll(drive, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
root='` + strings.ReplaceAll(drive, "'", `'\''`) + `'
cmd=$1; shift
if [ "$cmd" = "--version" ]; then echo "rclone v1.70.9"; exit 0; fi
local_src=""; remote=""; last=""
while [ $# -gt 0 ]; do
	case "$1" in
	--config) shift 2 ;;
	--*) shift ;;
	*) last=$1; if [ -z "$local_src" ]; then local_src=$1; else remote=$1; fi; shift ;;
	esac
done
case "$cmd" in
lsf|deletefile|lsd|lsjson) remote=$last ;;
esac
rel=${remote#*:}
case "$cmd" in
copyto)
	if [ "${local_src#vpsadmin:}" != "$local_src" ]; then
		# download: the remote is the source
		cp "$root/${local_src#*:}" "$remote"
	else
		mkdir -p "$root/$(dirname "$rel")"
		cp "$local_src" "$root/$rel"
	fi
	;;
lsf)
	dir="$root/$rel"
	[ -d "$dir" ] || exit 0
	(cd "$dir" && ls -1)
	;;
deletefile)
	rm -f "$root/$rel"
	;;
lsd)
	dir="$root/$rel"
	[ -d "$dir" ] || exit 0
	for d in "$dir"/*; do
		[ -d "$d" ] && printf 'd 0 2026-01-01 00:00:00        -1 %s\n' "$(basename "$d")"
	done
	;;
lsjson)
	dir="$root/$rel"
	if [ ! -d "$dir" ]; then echo "[]"; exit 0; fi
	printf '['
	sep=''
	for f in "$dir"/*; do
		[ -f "$f" ] || continue
		printf '%s{"Name":"%s","Size":%s,"ModTime":"2026-10-24T13:05:00Z"}' "$sep" "$(basename "$f")" "$(wc -c <"$f")"
		sep=','
	done
	printf ']\n'
	;;
*)
	echo "fake rclone: unsupported $cmd" >&2
	exit 1
	;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "rclone"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	return drive
}

func strptr(s string) *string { return &s }
func intptr(i int) *int       { return &i }

func newTestManager(t *testing.T, dir string) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	box, err := secret.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{
		Store: st, Box: box, Version: "1.2.3",
		Cfg: &config.Config{DataDir: dir, PublicURL: "https://panel.example"},
	}
	if err := m.Save(context.Background(), Options{
		Token:      strptr(`{"access_token":"ya29","token_type":"Bearer","refresh_token":"1//r","expiry":"2026-10-24T15:00:00Z"}`),
		Passphrase: strptr("a long enough passphrase"),
	}); err != nil {
		t.Fatal(err)
	}
	return m, st
}

func TestManagerBacksUpAndRestores(t *testing.T) {
	dir := t.TempDir()
	drive := fakeRclone(t)
	ctx := context.Background()
	m, st := newTestManager(t, dir)
	defer st.Close()

	if !m.Configured(ctx) {
		t.Fatal("a token and a passphrase make the backup configured")
	}
	if _, err := os.Stat(filepath.Join(dir, confName)); err != nil {
		t.Fatalf("the rclone configuration was not written: %v", err)
	}
	if err := st.AddApp(ctx, &store.App{ID: "web", Repo: "acme/site", InstallationID: 7, Branch: "main",
		Build: deploy.BuildAuto, Keep: 5, Access: deploy.AccessPublic}); err != nil {
		t.Fatal(err)
	}

	name, size, err := m.run(ctx, settingsID)
	if err != nil {
		t.Fatal(err)
	}
	if size < 1000 {
		t.Fatalf("the archive looks empty (%d bytes)", size)
	}
	if _, err := os.Stat(filepath.Join(drive, DefaultPath, name)); err != nil {
		t.Fatalf("the archive did not reach the remote: %v", err)
	}
	archives, err := m.Archives(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 || archives[0].Name != name {
		t.Fatalf("Archives returned %+v", archives)
	}

	// A wrong passphrase leaves no staged restore behind.
	if _, err := m.Restore(ctx, name, "not the passphrase"); err == nil {
		t.Fatal("a wrong passphrase must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "restore", "vps-admin.db")); !os.IsNotExist(err) {
		t.Fatal("nothing should have been staged")
	}

	h, err := m.Restore(ctx, name, "a long enough passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if h.Kind != settingsID || h.Panel != "1.2.3" {
		t.Fatalf("header came back as %+v", h)
	}
	if _, err := os.Stat(filepath.Join(dir, "restore", "secret.key")); err != nil {
		t.Fatalf("the key must be staged with the database: %v", err)
	}

	// Restoring means a restart: close the store, then apply.
	st.Close()
	if err := ApplyPending(dir); err != nil {
		t.Fatal(err)
	}
	again, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if _, err := again.App(ctx, "web"); err != nil {
		t.Fatalf("the restored database lost its apps: %v", err)
	}
}

func TestManagerPrunesOldArchives(t *testing.T) {
	dir := t.TempDir()
	fakeRclone(t)
	ctx := context.Background()
	m, st := newTestManager(t, dir)
	defer st.Close()
	if err := m.Save(ctx, Options{Keep: intptr(1)}); err != nil {
		t.Fatal(err)
	}
	first, _, err := m.run(ctx, settingsID)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // the archive name has a one-second resolution
	if _, _, err := m.run(ctx, settingsID); err != nil {
		t.Fatal(err)
	}
	archives, err := m.Archives(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 || archives[0].Name == first {
		t.Fatalf("keep=1 must leave only the newest archive, got %+v", archives)
	}
}

func TestManagerDeletesAnArchive(t *testing.T) {
	dir := t.TempDir()
	drive := fakeRclone(t)
	ctx := context.Background()
	m, st := newTestManager(t, dir)
	defer st.Close()

	name, _, err := m.run(ctx, settingsID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(drive, DefaultPath, name)); !os.IsNotExist(err) {
		t.Fatalf("the archive is still on the Drive: %v", err)
	}
	if list, err := m.Archives(ctx); err != nil || len(list) != 0 {
		t.Fatalf("Archives after delete = %+v, %v", list, err)
	}
	for _, bad := range []string{"../../etc/passwd", "notes.txt", "vps-admin-settings-nope.vpb"} {
		if err := m.Delete(ctx, bad); err == nil {
			t.Errorf("%q should have been refused", bad)
		}
	}
}

func TestArchivesAreNewestFirst(t *testing.T) {
	dir := t.TempDir()
	drive := fakeRclone(t)
	ctx := context.Background()
	m, st := newTestManager(t, dir)
	defer st.Close()

	folder := filepath.Join(drive, DefaultPath)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"vps-admin-settings-20260101T120000.vpb",
		"vps-admin-settings-20260301T120000.vpb",
		"vps-admin-settings-20260201T120000.vpb",
	} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("archive"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	list, err := m.Archives(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Name != "vps-admin-settings-20260301T120000.vpb" || list[2].Name != "vps-admin-settings-20260101T120000.vpb" {
		t.Fatalf("archives are not newest first: %+v", list)
	}
}

func TestManagerRefusesBadSettings(t *testing.T) {
	dir := t.TempDir()
	m, st := newTestManager(t, dir)
	defer st.Close()
	ctx := context.Background()
	cases := []Options{
		{Path: strptr("../etc")},
		{Path: strptr("a/../b")},
		{Path: strptr("a//b")},
		{Path: strptr("a b")},
		{Hour: intptr(24)},
		{Keep: intptr(0)},
		{Keep: intptr(200)},
		{Passphrase: strptr("short")},
		{Token: strptr(`{"access_token":"x"}`)},
		{Kind: strptr("everything")},
	}
	for _, o := range cases {
		if err := m.Save(ctx, o); err == nil {
			t.Errorf("%+v should have been refused", o)
		}
	}
	if got := m.remotePath(ctx); got != DefaultPath {
		t.Fatalf("a refused path must not be stored, got %q", got)
	}
}

func TestStatusWithoutRcloneSaysSo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", filepath.Join(dir, "empty"))
	m, st := newTestManager(t, dir)
	defer st.Close()
	s := m.Status(context.Background(), false)
	if s.Rclone != "" || s.Warning == "" {
		t.Fatalf("without rclone the status must warn, got %+v", s)
	}
}

func TestSaveClearsTheToken(t *testing.T) {
	dir := t.TempDir()
	m, st := newTestManager(t, dir)
	defer st.Close()
	ctx := context.Background()
	if err := m.Save(ctx, Options{Token: strptr("  ")}); err != nil {
		t.Fatal(err)
	}
	if m.Configured(ctx) {
		t.Fatal("an empty token must disconnect the Drive")
	}
	if _, err := os.Stat(filepath.Join(dir, confName)); !os.IsNotExist(err) {
		t.Fatal("the rclone configuration must go with the token")
	}
}

func TestSetMountNeedsTheHelper(t *testing.T) {
	dir := t.TempDir()
	m, st := newTestManager(t, dir)
	defer st.Close()
	if err := m.SetMount(context.Background(), true); err == nil {
		t.Fatal("without the helper the mount must be refused")
	}
}

func TestScheduledHourIsClampedOnRead(t *testing.T) {
	dir := t.TempDir()
	m, st := newTestManager(t, dir)
	defer st.Close()
	ctx := context.Background()
	m.Store.SetSetting(ctx, SettingHour, "99")
	if got := m.hour(ctx); got != DefaultHour {
		t.Fatalf("a stored hour out of range must fall back, got %d", got)
	}
	m.Store.SetSetting(ctx, SettingKeep, "0")
	if got := m.keep(ctx); got != DefaultKeep {
		t.Fatalf("a stored keep out of range must fall back, got %d", got)
	}
	if !m.mountOn(ctx) {
		t.Fatal("the Drive mounts at boot by default")
	}
	if got := m.kind(ctx); got != settingsID {
		t.Fatalf("the daily backup is settings only by default, got %q", got)
	}
}
