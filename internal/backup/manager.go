package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
	"github.com/beedywool/vps-admin/internal/system"

	_ "modernc.org/sqlite"
)

const (
	// The Drive remote and its mount point are fixed: the administrator never chooses
	// them, so they cannot end up in a path the root helper would write to.
	RemoteName = "vpsadmin"
	MountPoint = "/srv/storage/vpsadmin"
	HelperPath = "/opt/vps-admin/storage-helper"

	confName      = "rclone.conf"
	settingsID    = "settings" // backup kind: the panel state
	fullID        = "full"     // backup kind: state plus app sources and volumes
	minPassphrase = 12
	maxKeep       = 60
	// A full archive copies app sources and volumes: allow it to run long.
	runTimeout = 4 * time.Hour
)

// Settings keys, in the settings table next to the others.
const (
	SettingEnabled    = "backup.enabled"
	SettingToken      = "backup.token"      // sealed: the rclone OAuth token
	SettingPassphrase = "backup.passphrase" // sealed: what encrypts the archives
	SettingPath       = "backup.path"
	SettingHour       = "backup.hour"
	SettingKeep       = "backup.keep"
	SettingMount      = "backup.mount"
	SettingKind       = "backup.kind" // settings | full: what the daily run writes
	SettingLast       = "backup.last"
)

// Defaults, exported so the panel and the tests agree.
const (
	DefaultPath = "vps-admin"
	DefaultHour = 3
	DefaultKeep = 7
	DefaultKind = settingsID
)

var (
	httpsRemotePath = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,119}$`)
	archiveFile     = regexp.MustCompile(`^vps-admin-(settings|full)-\d{8}T\d{6}\.vpb$`)
)

// Archive is one file on the remote.
type Archive struct {
	Name string    `json:"name"`
	Size int64     `json:"size"`
	Time time.Time `json:"time"`
}

// Last is the outcome of the latest run, kept in the database so the panel can show it
// after a restart.
type Last struct {
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`
	File  string    `json:"file"`
	Size  int64     `json:"size"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

// Status is what the settings page shows.
type Status struct {
	Configured    bool      `json:"configured"`
	Enabled       bool      `json:"enabled"`
	HasToken      bool      `json:"hasToken"`
	HasPassphrase bool      `json:"hasPassphrase"`
	Path          string    `json:"path"`
	Hour          int       `json:"hour"`
	Keep          int       `json:"keep"`
	Kind          string    `json:"kind"` // what the daily run writes
	Mount         bool      `json:"mount"`
	MountPoint    string    `json:"mountPoint"`
	Mounted       bool      `json:"mounted"`
	Helper        bool      `json:"helper"`
	Rclone        string    `json:"rclone"` // version, empty when not installed
	Running       bool      `json:"running"`
	Progress      string    `json:"progress"`
	RestoreStaged bool      `json:"restoreStaged"`
	Last          *Last     `json:"last,omitempty"`
	Archives      []Archive `json:"archives"`
	Warning       string    `json:"warning,omitempty"`
}

// Options are the values the panel may change. Nil means "leave it alone", so an empty
// password field does not erase the one saved.
type Options struct {
	Enabled    *bool   `json:"enabled"`
	Path       *string `json:"path"`
	Hour       *int    `json:"hour"`
	Keep       *int    `json:"keep"`
	Kind       *string `json:"kind"`
	Mount      *bool   `json:"mount"`
	Token      *string `json:"token"`
	Passphrase *string `json:"passphrase"`
}

// Manager owns the Drive connection, the mount and the backup runs.
type Manager struct {
	Store   *store.Store
	Box     *secret.Box
	Runner  *system.Runner
	Cfg     *config.Config
	Version string
	Helper  string // path of the root helper, empty when the installer did not put it
	Docker  *docker.Client

	// Tests replace the volume listing and the tar/unpack of a volume; in production
	// they go through Docker and the root helper.
	Volumes        func(context.Context) ([]docker.VolumeUse, error)
	exportVolumeFn func(context.Context, string, string) error
	importVolumeFn func(context.Context, string, string) error

	mu         sync.Mutex
	running    bool
	progress   string
	mountTried time.Time
}

// Configured reports whether a token and a passphrase are saved: without both, a backup
// cannot be written.
func (m *Manager) Configured(ctx context.Context) bool {
	return m.Store.Setting(ctx, SettingToken, "") != "" && m.Store.Setting(ctx, SettingPassphrase, "") != ""
}

func (m *Manager) enabled(ctx context.Context) bool {
	return m.Store.Setting(ctx, SettingEnabled, "false") == "true"
}

// remotePath is the folder inside Drive, relative and validated. A bare name check is
// not enough: a mounted Drive is a folder, and .. would write outside it.
func (m *Manager) remotePath(ctx context.Context) string {
	p := strings.Trim(m.Store.Setting(ctx, SettingPath, DefaultPath), "/")
	if !validPath(p) {
		return DefaultPath
	}
	return p
}

func validPath(p string) bool {
	if !httpsRemotePath.MatchString(p) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (m *Manager) hour(ctx context.Context) int {
	h, err := strconv.Atoi(m.Store.Setting(ctx, SettingHour, strconv.Itoa(DefaultHour)))
	if err != nil || h < 0 || h > 23 {
		return DefaultHour
	}
	return h
}

func (m *Manager) keep(ctx context.Context) int {
	k, err := strconv.Atoi(m.Store.Setting(ctx, SettingKeep, strconv.Itoa(DefaultKeep)))
	if err != nil || k < 1 {
		return DefaultKeep
	}
	if k > maxKeep {
		return maxKeep
	}
	return k
}

func (m *Manager) mountOn(ctx context.Context) bool {
	return m.Store.Setting(ctx, SettingMount, "true") == "true"
}

func (m *Manager) kind(ctx context.Context) string {
	if m.Store.Setting(ctx, SettingKind, DefaultKind) == fullID {
		return fullID
	}
	return settingsID
}

func (m *Manager) token(ctx context.Context) (string, error) {
	return m.Box.Open(m.Store.Setting(ctx, SettingToken, ""))
}

func (m *Manager) passphrase(ctx context.Context) (string, error) {
	return m.Box.Open(m.Store.Setting(ctx, SettingPassphrase, ""))
}

func (m *Manager) confPath() string { return filepath.Join(m.Cfg.DataDir, confName) }

// Save applies what the panel sent. The token is normalized: `rclone authorize` prints
// it between markers, and people paste those too.
func (m *Manager) Save(ctx context.Context, o Options) error {
	if o.Path != nil {
		p := strings.Trim(strings.TrimSpace(*o.Path), "/")
		if !validPath(p) {
			return errors.New("the folder must be a plain path like vps-admin/backups")
		}
		if err := m.Store.SetSetting(ctx, SettingPath, p); err != nil {
			return err
		}
	}
	if o.Hour != nil {
		if *o.Hour < 0 || *o.Hour > 23 {
			return errors.New("the hour must be between 0 and 23")
		}
		if err := m.Store.SetSetting(ctx, SettingHour, strconv.Itoa(*o.Hour)); err != nil {
			return err
		}
	}
	if o.Keep != nil {
		if *o.Keep < 1 || *o.Keep > maxKeep {
			return fmt.Errorf("keep between 1 and %d archives", maxKeep)
		}
		if err := m.Store.SetSetting(ctx, SettingKeep, strconv.Itoa(*o.Keep)); err != nil {
			return err
		}
	}
	if o.Kind != nil {
		if *o.Kind != settingsID && *o.Kind != fullID {
			return errors.New("the scheduled backup must be settings or full")
		}
		if err := m.Store.SetSetting(ctx, SettingKind, *o.Kind); err != nil {
			return err
		}
	}
	if o.Mount != nil {
		if err := m.Store.SetSetting(ctx, SettingMount, strconv.FormatBool(*o.Mount)); err != nil {
			return err
		}
	}
	if o.Enabled != nil {
		if err := m.Store.SetSetting(ctx, SettingEnabled, strconv.FormatBool(*o.Enabled)); err != nil {
			return err
		}
	}
	if o.Token != nil {
		token, err := NormalizeToken(*o.Token)
		switch {
		case errors.Is(err, errNoToken):
			if err := m.Store.DeleteSetting(ctx, SettingToken); err != nil {
				return err
			}
			os.Remove(m.confPath())
		case err != nil:
			return err
		default:
			// The configuration goes first: a saved token the mount cannot read would
			// report a Drive that never comes up.
			if err := m.writeConf(token); err != nil {
				return err
			}
			if err := m.Store.SetSetting(ctx, SettingToken, m.Box.Seal(token)); err != nil {
				return err
			}
		}
	}
	if o.Passphrase != nil {
		p := *o.Passphrase
		if p == "" {
			return m.Store.DeleteSetting(ctx, SettingPassphrase)
		}
		if len([]rune(p)) < minPassphrase {
			return fmt.Errorf("the passphrase must be at least %d characters", minPassphrase)
		}
		return m.Store.SetSetting(ctx, SettingPassphrase, m.Box.Seal(p))
	}
	return nil
}

var errNoToken = errors.New("no token")

// NormalizeToken reads the JSON printed by `rclone authorize "drive"`, with or without
// the markers around it, and checks it can be used unattended (it needs a refresh token).
func NormalizeToken(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errNoToken
	}
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return "", errors.New("paste the JSON that `rclone authorize \"drive\"` prints, starting with {")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(raw[start:end+1]), &v); err != nil {
		return "", errors.New("that token is not valid JSON")
	}
	if keep, _ := v["refresh_token"].(string); strings.TrimSpace(keep) == "" {
		return "", errors.New("this token has no refresh_token: run `rclone authorize \"drive\"` again")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// writeConf keeps the rclone configuration in the data directory, 0600 like the secrets
// it holds. The mount unit reads it there.
func (m *Manager) writeConf(token string) error {
	if err := os.MkdirAll(m.Cfg.DataDir, 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf("[%s]\ntype = drive\nscope = drive\ntoken = %s\n", RemoteName, token)
	tmp := m.confPath() + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.confPath())
}

// SetMount mounts or unmounts the Drive through the root helper. The panel runs
// unprivileged, so it cannot write the systemd unit itself.
func (m *Manager) SetMount(ctx context.Context, on bool) error {
	if m.Helper == "" {
		return errors.New("the storage helper is missing: rerun the installer")
	}
	action := "umount"
	if on {
		action = "mount"
	}
	if _, err := m.sudo(ctx, 2*time.Minute, m.Helper, action, RemoteName); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if m.Mounted() == on {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if on {
		return fmt.Errorf("the Drive did not mount: journalctl -u rclone-%s", RemoteName)
	}
	return fmt.Errorf("the Drive is still busy: fusermount3 -uz %s over SSH", MountPoint)
}

// Mounted reads /proc/self/mounts: it is the panel's view, so it stays true for the
// containers and the backup writer alike.
func (m *Manager) Mounted() bool {
	b, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) > 1 && f[1] == MountPoint {
			return true
		}
	}
	return false
}

func (m *Manager) rcloneVersion(ctx context.Context) string {
	if _, err := exec.LookPath("rclone"); err != nil {
		return ""
	}
	out, err := m.exec(ctx, 5*time.Second, "rclone", "--version")
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return line
}

func (m *Manager) host() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// Status gathers what the settings page shows. Listing the remote needs network access,
// so it only happens when the Drive is mounted or the caller asks for it.
func (m *Manager) Status(ctx context.Context, withArchives bool) Status {
	st := Status{
		Configured:    m.Configured(ctx),
		Enabled:       m.enabled(ctx),
		HasToken:      m.Store.Setting(ctx, SettingToken, "") != "",
		HasPassphrase: m.Store.Setting(ctx, SettingPassphrase, "") != "",
		Path:          m.remotePath(ctx),
		Hour:          m.hour(ctx),
		Keep:          m.keep(ctx),
		Kind:          m.kind(ctx),
		Mount:         m.mountOn(ctx),
		MountPoint:    MountPoint,
		Mounted:       m.Mounted(),
		Helper:        m.Helper != "",
		Rclone:        m.rcloneVersion(ctx),
		Archives:      []Archive{},
	}
	if raw := m.Store.Setting(ctx, SettingLast, ""); raw != "" {
		var last Last
		if json.Unmarshal([]byte(raw), &last) == nil {
			st.Last = &last
		}
	}
	if _, err := os.Stat(filepath.Join(m.Cfg.DataDir, "restore", "vps-admin.db")); err == nil {
		st.RestoreStaged = true
	}
	m.mu.Lock()
	st.Running, st.Progress = m.running, m.progress
	m.mu.Unlock()
	switch {
	case st.Rclone == "":
		st.Warning = "rclone is not installed: rerun the installer to add it"
	case !st.Helper:
		st.Warning = "the storage helper is missing: rerun the installer"
	}
	if withArchives || st.Mounted {
		if list, err := m.Archives(ctx); err == nil {
			st.Archives = list
		}
	}
	return st
}

func (m *Manager) saveLast(ctx context.Context, last Last) {
	b, err := json.Marshal(last)
	if err != nil {
		return
	}
	if err := m.Store.SetSetting(context.WithoutCancel(ctx), SettingLast, string(b)); err != nil {
		slog.Warn("backup: cannot save the last run", "err", err)
	}
}

func (m *Manager) setProgress(p string) {
	m.mu.Lock()
	m.progress = p
	m.mu.Unlock()
}

// Start runs a backup in the background and returns at once: the panel polls the status
// for the progress and the outcome.
func (m *Manager) Start(ctx context.Context, kind string) error {
	if kind != settingsID && kind != fullID {
		return errors.New("unknown backup kind")
	}
	if !m.Configured(ctx) {
		return errors.New("connect Google Drive and choose a passphrase first")
	}
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return errors.New("a backup is already running")
	}
	m.running, m.progress = true, "Starting"
	m.mu.Unlock()

	go func() {
		runCtx, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()
		start := time.Now().UTC()
		name, size, err := m.run(runCtx, kind)
		last := Last{At: start, Kind: kind, File: name, Size: size, OK: err == nil}
		if err != nil {
			last.Error = err.Error()
			slog.Error("backup failed", "kind", kind, "err", err)
		} else {
			slog.Info("backup written", "kind", kind, "file", name, "bytes", size)
		}
		m.saveLast(runCtx, last)
		m.mu.Lock()
		m.running, m.progress = false, ""
		m.mu.Unlock()
	}()
	return nil
}

// run writes the archive and uploads it. It returns what was uploaded, for the history.
func (m *Manager) run(ctx context.Context, kind string) (string, int64, error) {
	work, err := os.MkdirTemp(m.Cfg.DataDir, "backup-")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(work)

	m.setProgress("Snapshotting the database")
	snapshot := filepath.Join(work, "vps-admin.db")
	if err := m.Store.Snapshot(ctx, snapshot); err != nil {
		return "", 0, fmt.Errorf("database snapshot: %w", err)
	}
	key, err := os.ReadFile(filepath.Join(m.Cfg.DataDir, "secret.key"))
	if err != nil {
		return "", 0, fmt.Errorf("secret.key: %w", err)
	}
	pass, err := m.passphrase(ctx)
	if err != nil || pass == "" {
		return "", 0, errors.New("the passphrase cannot be read: save it again")
	}

	name := ArchiveName(kind, time.Now())
	dest := filepath.Join(work, name)
	h := Header{Kind: kind, Created: time.Now().UTC(), Panel: m.Version, Host: m.host(), PublicURL: m.Cfg.PublicURL}
	entries := []Entry{
		// The key travels with the database: a restore on a fresh server must be able to
		// read the variables the database holds, and the archive is encrypted anyway.
		{Name: "vps-admin.db", Path: snapshot, Mode: 0o600},
		{Name: "secret.key", Data: key, Mode: 0o600},
	}
	if kind == fullID {
		extra, err := m.collectFull(ctx, work)
		if err != nil {
			return name, 0, err
		}
		entries = append(entries, extra...)
	}
	m.setProgress("Encrypting the archive")
	if err := Write(dest, pass, h, entries); err != nil {
		return name, 0, err
	}
	st, err := os.Stat(dest)
	if err != nil {
		return name, 0, err
	}

	m.setProgress("Uploading to Google Drive")
	if err := m.upload(ctx, dest, name); err != nil {
		return name, st.Size(), err
	}
	m.setProgress("Removing old archives")
	if err := m.prune(ctx); err != nil {
		// The backup is on the Drive: a failed cleanup is not a failed backup.
		slog.Warn("backup: cannot prune old archives", "err", err)
	}
	return name, st.Size(), nil
}

// upload prefers the mount (the user asked for it, and it keeps one copy on the Drive),
// then falls back to rclone copying straight to the remote when the mount is down.
func (m *Manager) upload(ctx context.Context, local, name string) error {
	if m.Mounted() {
		dir := filepath.Join(MountPoint, m.remotePath(ctx))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		tmp := filepath.Join(dir, "."+name+".part")
		if err := copyFile(local, tmp); err != nil {
			os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, filepath.Join(dir, name))
	}
	if _, err := exec.LookPath("rclone"); err != nil {
		return errors.New("Google Drive is not mounted and rclone is not installed")
	}
	remote := RemoteName + ":" + m.remotePath(ctx) + "/" + name
	_, err := m.exec(ctx, time.Hour, "rclone", "copyto", "--drive-use-trash=false",
		"--config", m.confPath(), local, remote)
	return err
}

// listNames returns the archive names on the remote, newest last.
func (m *Manager) listNames(ctx context.Context) ([]string, error) {
	if m.Mounted() {
		entries, err := os.ReadDir(filepath.Join(MountPoint, m.remotePath(ctx)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		var names []string
		for _, e := range entries {
			if !e.IsDir() && archiveFile.MatchString(e.Name()) {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		return names, nil
	}
	if _, err := exec.LookPath("rclone"); err != nil {
		return nil, errors.New("rclone is not installed")
	}
	out, err := m.exec(ctx, time.Minute, "rclone", "lsf", "--files-only",
		"--config", m.confPath(), RemoteName+":"+m.remotePath(ctx))
	if err != nil {
		return nil, err
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if archiveFile.MatchString(l) {
			names = append(names, l)
		}
	}
	sort.Strings(names)
	return names, nil
}

// Archives lists what can be restored, with sizes and dates when the Drive is mounted.
func (m *Manager) Archives(ctx context.Context) ([]Archive, error) {
	var out []Archive
	if m.Mounted() {
		dir := filepath.Join(MountPoint, m.remotePath(ctx))
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return []Archive{}, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !archiveFile.MatchString(e.Name()) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, Archive{Name: e.Name(), Size: info.Size(), Time: info.ModTime()})
		}
	} else {
		// Without the mount, lsjson is what gives sizes and dates; lsf would only give
		// names, and the panel shows a date next to every archive.
		if _, err := exec.LookPath("rclone"); err != nil {
			return nil, errors.New("rclone is not installed")
		}
		raw, err := m.exec(ctx, time.Minute, "rclone", "lsjson", "--files-only",
			"--config", m.confPath(), RemoteName+":"+m.remotePath(ctx))
		if err != nil {
			return nil, err
		}
		var entries []struct {
			Name    string    `json:"Name"`
			Size    int64     `json:"Size"`
			ModTime time.Time `json:"ModTime"`
		}
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return nil, fmt.Errorf("rclone lsjson: %w", err)
		}
		for _, e := range entries {
			if archiveFile.MatchString(e.Name) {
				out = append(out, Archive{Name: e.Name, Size: e.Size, Time: e.ModTime})
			}
		}
	}
	// The tab shows the list as it comes: newest first, then by name when the Drive gives
	// the same modification time to several files.
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.After(out[j].Time)
		}
		return out[i].Name > out[j].Name
	})
	return out, nil
}

// Delete removes one archive from the Drive. The panel asks for confirmation first: the
// file no longer appears in the list, and a backup cannot be restored without it.
func (m *Manager) Delete(ctx context.Context, name string) error {
	if !archiveFile.MatchString(name) {
		return errors.New("that is not a backup file name")
	}
	m.mu.Lock()
	running := m.running
	m.mu.Unlock()
	if running {
		return errors.New("wait for the running backup to finish")
	}
	return m.remove(ctx, name)
}

// remove deletes one archive through the mount when it is up, and with rclone otherwise.
func (m *Manager) remove(ctx context.Context, name string) error {
	if m.Mounted() {
		err := os.Remove(filepath.Join(MountPoint, m.remotePath(ctx), name))
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("this archive is already gone")
		}
		return err
	}
	if _, err := exec.LookPath("rclone"); err != nil {
		return errors.New("rclone is not installed: rerun the installer")
	}
	_, err := m.exec(ctx, time.Minute, "rclone", "deletefile", "--drive-use-trash=false",
		"--config", m.confPath(), RemoteName+":"+m.remotePath(ctx)+"/"+name)
	return err
}

// prune keeps the newest archives and removes the rest, from Drive too (no trash, so
// the space comes back).
func (m *Manager) prune(ctx context.Context) error {
	names, err := m.listNames(ctx)
	if err != nil {
		return err
	}
	keep := m.keep(ctx)
	if len(names) <= keep {
		return nil
	}
	for _, name := range names[:len(names)-keep] {
		if err := m.remove(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// Test checks the Drive answers with the saved token, and reports what it finds.
func (m *Manager) Test(ctx context.Context) (string, error) {
	if _, err := m.token(ctx); err != nil {
		return "", errors.New("the saved token cannot be read: paste it again")
	}
	if m.Mounted() {
		names, err := m.listNames(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Google Drive is mounted, %d archive(s) in %s", len(names), m.remotePath(ctx)), nil
	}
	if _, err := exec.LookPath("rclone"); err != nil {
		return "", errors.New("rclone is not installed: rerun the installer")
	}
	// The backup folder may not exist yet: checking the Drive root proves the token
	// without failing on a fresh setup.
	if _, err := m.exec(ctx, time.Minute, "rclone", "lsd", "--config", m.confPath(), RemoteName+":"); err != nil {
		return "", err
	}
	return fmt.Sprintf("Google Drive answers; backups go to %s", m.remotePath(ctx)), nil
}

// Restore downloads an archive, checks it and stages it for the next start. Replacing
// the database of a running panel is not safe, so the caller restarts the service and
// ApplyPending swaps the files in before the store opens.
func (m *Manager) Restore(ctx context.Context, file, passphrase string) (Header, error) {
	var h Header
	if !archiveFile.MatchString(file) {
		return h, errors.New("that is not a backup file name")
	}
	if strings.TrimSpace(passphrase) == "" {
		return h, errors.New("the passphrase is needed to open this archive")
	}
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return h, errors.New("wait for the running backup to finish")
	}
	m.mu.Unlock()

	work, err := os.MkdirTemp(m.Cfg.DataDir, "restore-")
	if err != nil {
		return h, err
	}
	defer os.RemoveAll(work)
	local := filepath.Join(work, file)
	if m.Mounted() {
		src := filepath.Join(MountPoint, m.remotePath(ctx), file)
		if err := copyFile(src, local); err != nil {
			return h, err
		}
	} else {
		if _, err := exec.LookPath("rclone"); err != nil {
			return h, errors.New("rclone is not installed and the Drive is not mounted")
		}
		if _, err := m.exec(ctx, time.Hour, "rclone", "copyto", "--config", m.confPath(),
			RemoteName+":"+m.remotePath(ctx)+"/"+file, local); err != nil {
			return h, err
		}
	}

	dir := filepath.Join(work, "payload")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return h, err
	}
	h, names, err := Extract(local, passphrase, dir)
	if err != nil {
		return h, err
	}
	hasDB := false
	for _, n := range names {
		if n == "vps-admin.db" {
			hasDB = true
		}
	}
	if !hasDB {
		return h, errors.New("this archive holds no database")
	}
	if err := validSnapshot(filepath.Join(dir, "vps-admin.db")); err != nil {
		return h, err
	}

	// Stage the files: the panel replaces them at its next start, never while it runs.
	stage := filepath.Join(m.Cfg.DataDir, "restore")
	if err := os.RemoveAll(stage); err != nil {
		return h, err
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return h, err
	}
	for _, n := range []string{"vps-admin.db", "secret.key"} {
		src := filepath.Join(dir, n)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(stage, n)); err != nil {
			return h, err
		}
	}
	if h.Kind == fullID {
		if err := restoreFull(stage, dir, names); err != nil {
			return h, err
		}
	}
	return h, nil
}

// ApplyPending installs a staged restore. It runs before the store opens: the database
// and the key are swapped together, and the previous files stay next to them with a
// timestamp.
func ApplyPending(dataDir string) error {
	stage := filepath.Join(dataDir, "restore")
	db := filepath.Join(stage, "vps-admin.db")
	if _, err := os.Stat(db); err != nil {
		return nil
	}
	stamp := time.Now().UTC().Format("20060102T150405")
	for _, f := range []string{"vps-admin.db", "vps-admin.db-wal", "vps-admin.db-shm"} {
		src := filepath.Join(dataDir, f)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, src+".before-restore-"+stamp); err != nil {
			return err
		}
	}
	if err := os.Rename(db, filepath.Join(dataDir, "vps-admin.db")); err != nil {
		return err
	}
	if err := applyFullFiles(dataDir, stage, stamp); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(stage, "secret.key")); err == nil {
		key := filepath.Join(dataDir, "secret.key")
		if _, err := os.Stat(key); err == nil {
			os.Rename(key, key+".before-restore-"+stamp)
		}
		if err := os.Rename(filepath.Join(stage, "secret.key"), key); err != nil {
			return err
		}
	}
	// The volumes are unpacked by ApplyStagedVolumes once the Docker client is up; the
	// rest of the stage goes now.
	if _, err := os.Stat(filepath.Join(stage, "volumes")); err == nil {
		slog.Info("full restore staged", "volumes", filepath.Join(stage, "volumes"))
		return nil
	}
	if err := os.RemoveAll(stage); err != nil {
		return err
	}
	slog.Info("backup restored", "database", filepath.Join(dataDir, "vps-admin.db"), "previous", ".before-restore-"+stamp)
	return nil
}

// Loop runs the scheduled backup. It checks every minute: a missed hour (the panel was
// down) is caught up the same day, and a failure does not retry in a loop.
func (m *Manager) Loop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !m.enabled(ctx) || !m.Configured(ctx) {
			continue
		}
		// The Drive should come back after a reboot; the unit does it, but a unit that was
		// stopped or removed is retried here, at most every ten minutes.
		if m.mountOn(ctx) && m.Helper != "" && !m.Mounted() {
			m.mu.Lock()
			due := time.Since(m.mountTried) > 10*time.Minute
			if due {
				m.mountTried = time.Now()
			}
			m.mu.Unlock()
			if due {
				if err := m.SetMount(ctx, true); err != nil {
					slog.Warn("backup: cannot mount the Drive", "err", err)
				}
			}
		}
		now := time.Now()
		if now.Hour() != m.hour(ctx) {
			continue
		}
		var last Last
		if raw := m.Store.Setting(ctx, SettingLast, ""); raw != "" {
			if json.Unmarshal([]byte(raw), &last) != nil {
				continue
			}
			if last.At.In(time.Local).Format("2006-01-02") == now.Format("2006-01-02") {
				continue
			}
		}
		if err := m.Start(ctx, m.kind(ctx)); err != nil {
			slog.Warn("scheduled backup not started", "err", err)
		}
	}
}

func (m *Manager) exec(ctx context.Context, timeout time.Duration, argv ...string) (string, error) {
	if m.Runner != nil {
		return m.Runner.Run(ctx, timeout, argv...)
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(c, argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w: %s", argv[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (m *Manager) sudo(ctx context.Context, timeout time.Duration, argv ...string) (string, error) {
	if m.Runner != nil {
		return m.Runner.Sudo(ctx, timeout, argv...)
	}
	if os.Geteuid() == 0 {
		return m.exec(ctx, timeout, argv...)
	}
	return m.exec(ctx, timeout, append([]string{"sudo", "-n", "--"}, argv...)...)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// validSnapshot makes sure a restored database is a readable panel state before it is
// allowed anywhere near the real one.
func validSnapshot(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	head := make([]byte, 16)
	_, rerr := io.ReadFull(f, head)
	f.Close()
	if rerr != nil || string(head) != "SQLite format 3\x00" {
		return errors.New("the archive does not hold a SQLite database")
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var check string
	if err := db.QueryRow(`PRAGMA integrity_check`).Scan(&check); err != nil || check != "ok" {
		return errors.New("the restored database is damaged")
	}
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('apps', 'settings')`).Scan(&tables); err != nil || tables < 2 {
		return errors.New("the archive is not a vps-admin database")
	}
	return nil
}
