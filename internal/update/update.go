// Package update installs new agent releases from GitHub.
//
// A release carries one binary per architecture, a SHA256SUMS file and SHA256SUMS.sig, an
// ed25519 signature of that file made with the key whose public half is in pubkey.go.
// A binary that does not match a signed checksum is never installed, even if GitHub or
// the network is compromised.
//
// Rollback: before swapping binaries the agent writes an attempt counter. The systemd
// unit runs deploy/preflight.sh before each start; it increments the counter and puts the
// previous binary back after three failed starts. A new agent that stays up clears the
// counter with Commit. The rollback lives in that script because a broken binary cannot be
// trusted to undo itself.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beedywool/vps-admin/internal/buildinfo"
)

const (
	maxBinary   = 200 << 20
	pendingFile = "update-pending"
	targetFile  = "update-target" // tag being installed, for preflight.sh to report
	resultFile  = "update-result"
)

var (
	ErrNoRelease  = errors.New("no release available")
	ErrSignature  = errors.New("release signature does not match the embedded public key")
	ErrChecksum   = errors.New("downloaded binary does not match the signed checksum")
	ErrInProgress = errors.New("an update is already running")
)

type Release struct {
	Tag        string            `json:"tag"`
	Name       string            `json:"name"`
	Notes      string            `json:"notes"`
	Prerelease bool              `json:"prerelease"`
	Published  time.Time         `json:"published"`
	URL        string            `json:"url"`
	assets     map[string]string // name -> download URL
}

type Status struct {
	Current    string     `json:"current"`
	Latest     *Release   `json:"latest,omitempty"`
	Available  bool       `json:"available"`
	CheckedAt  *time.Time `json:"checkedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
	LastResult string     `json:"lastResult,omitempty"` // e.g. "rolled back v1.4.0"
	Running    bool       `json:"running"`
}

type Updater struct {
	Repo    string
	DataDir string
	// Binary is the path of the running executable, replaced in place.
	Binary string
	HTTP   *http.Client
	API    string // GitHub API root, changed by tests
	// Token authenticates against a private repository; assets are then fetched through
	// the API instead of the public download links.
	Token string

	mu      sync.Mutex
	status  Status
	running bool
}

func New(dataDir string) (*Updater, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return nil, err
	}
	u := &Updater{Repo: buildinfo.Repo, DataDir: dataDir, Binary: exe,
		HTTP: &http.Client{Timeout: 2 * time.Minute}, API: "https://api.github.com"}
	u.status.Current = buildinfo.Version
	if b, err := os.ReadFile(filepath.Join(dataDir, resultFile)); err == nil {
		u.status.LastResult = strings.TrimSpace(string(b))
	}
	return u, nil
}

func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.status
	s.Running = u.running
	return s
}

// Check asks GitHub for the newest release that applies to this install.
func (u *Updater) Check(ctx context.Context, prerelease bool) (Status, error) {
	rel, err := u.latest(ctx, prerelease)
	now := time.Now()
	u.mu.Lock()
	u.status.CheckedAt = &now
	if err != nil {
		u.status.Error = err.Error()
	} else {
		u.status.Error = ""
		u.status.Latest = rel
		u.status.Available = rel != nil && Newer(rel.Tag, buildinfo.Version)
	}
	u.mu.Unlock()
	return u.Status(), err
}

func (u *Updater) latest(ctx context.Context, prerelease bool) (*Release, error) {
	return u.latestWith(ctx, prerelease, func(*Release) bool { return true })
}

// latestWith returns the highest release accepted by keep.
func (u *Updater) latestWith(ctx context.Context, prerelease bool, keep func(*Release) bool) (*Release, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/releases?per_page=20", u.API, u.Repo), nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	u.auth(req)
	res, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered HTTP %d", res.StatusCode)
	}
	var list []struct {
		Tag        string    `json:"tag_name"`
		Name       string    `json:"name"`
		Body       string    `json:"body"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		Published  time.Time `json:"published_at"`
		HTMLURL    string    `json:"html_url"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			APIURL string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&list); err != nil {
		return nil, err
	}
	var best *Release
	for _, r := range list {
		if r.Draft || (r.Prerelease && !prerelease) || parse(r.Tag) == nil {
			continue
		}
		if best != nil && !Newer(r.Tag, best.Tag) {
			continue
		}
		rel := &Release{Tag: r.Tag, Name: r.Name, Notes: r.Body, Prerelease: r.Prerelease, Published: r.Published, URL: r.HTMLURL, assets: map[string]string{}}
		for _, a := range r.Assets {
			rel.assets[a.Name] = a.URL
			if u.Token != "" {
				rel.assets[a.Name] = a.APIURL
			}
		}
		if keep(rel) {
			best = rel
		}
	}
	return best, nil
}

// AssetName is the binary for this machine in a release.
func AssetName() string { return "vps-admin_linux_" + runtime.GOARCH }

// Apply downloads, verifies and installs rel. The caller then exits so systemd starts
// the new binary.
func (u *Updater) Apply(ctx context.Context, rel *Release) error {
	u.mu.Lock()
	if u.running {
		u.mu.Unlock()
		return ErrInProgress
	}
	u.running = true
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.running = false
		u.mu.Unlock()
	}()

	binURL, sumsURL, sigURL := rel.assets[AssetName()], rel.assets["SHA256SUMS"], rel.assets["SHA256SUMS.sig"]
	if binURL == "" || sumsURL == "" || sigURL == "" {
		return fmt.Errorf("release %s has no signed build for linux/%s", rel.Tag, runtime.GOARCH)
	}
	sums, err := u.fetch(ctx, sumsURL, 1<<20)
	if err != nil {
		return err
	}
	sig, err := u.fetch(ctx, sigURL, 4096)
	if err != nil {
		return err
	}
	want, err := VerifySums(sums, sig, AssetName())
	if err != nil {
		return err
	}

	dir := filepath.Dir(u.Binary)
	newPath := u.Binary + ".new"
	if err := u.download(ctx, binURL, newPath, want); err != nil {
		os.Remove(newPath)
		return err
	}
	// The new binary must at least run and report the expected version.
	out, err := exec.CommandContext(ctx, newPath, "version").Output()
	if err != nil || strings.TrimSpace(string(out)) != rel.Tag {
		os.Remove(newPath)
		return fmt.Errorf("the new binary does not start correctly (%v, %q)", err, strings.TrimSpace(string(out)))
	}

	if err := os.WriteFile(filepath.Join(u.DataDir, targetFile), []byte(rel.Tag+"\n"), 0o600); err != nil {
		os.Remove(newPath)
		return err
	}
	if err := os.WriteFile(filepath.Join(u.DataDir, pendingFile), []byte("0\n"), 0o600); err != nil {
		os.Remove(newPath)
		return err
	}
	os.Remove(filepath.Join(u.DataDir, resultFile))
	prev := filepath.Join(dir, filepath.Base(u.Binary)+".prev")
	if err := os.Rename(u.Binary, prev); err != nil {
		os.Remove(newPath)
		return err
	}
	if err := os.Rename(newPath, u.Binary); err != nil {
		os.Rename(prev, u.Binary)
		return err
	}
	return nil
}

// auth sets the user agent and, for private repositories, the token. Asset requests
// through the API need Accept: application/octet-stream to get the file itself.
func (u *Updater) auth(req *http.Request) {
	req.Header.Set("User-Agent", "vps-admin/"+buildinfo.Version)
	if u.Token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+u.Token)
	if strings.Contains(req.URL.Path, "/releases/assets/") {
		req.Header.Set("Accept", "application/octet-stream")
	}
}

func (u *Updater) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	u.auth(req)
	res, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", filepath.Base(url), res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, limit))
}

func (u *Updater) download(ctx context.Context, url, dst string, wantSum []byte) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	u.auth(req)
	res, err := u.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download binary: HTTP %d", res.StatusCode)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(res.Body, maxBinary))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != hex.EncodeToString(wantSum) {
		return ErrChecksum
	}
	return nil
}

// VerifySums checks the signature over a SHA256SUMS file and returns the hash listed
// for asset.
func VerifySums(sums, sig []byte, asset string) ([]byte, error) {
	pub, err := base64.StdEncoding.DecodeString(PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, errors.New("invalid embedded public key")
	}
	rawSig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || !ed25519.Verify(pub, sums, rawSig) {
		return nil, ErrSignature
	}
	for _, l := range strings.Split(string(sums), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == asset {
			return hex.DecodeString(f[0])
		}
	}
	return nil, fmt.Errorf("SHA256SUMS has no entry for %s", asset)
}

// Commit clears the pending marker once the new version has proven itself.
func (u *Updater) Commit() {
	p := filepath.Join(u.DataDir, pendingFile)
	if _, err := os.Stat(p); err != nil {
		return
	}
	os.Remove(p)
	os.Remove(filepath.Join(u.DataDir, targetFile))
	msg := "updated to " + buildinfo.Version
	os.WriteFile(filepath.Join(u.DataDir, resultFile), []byte(msg+"\n"), 0o600)
	u.mu.Lock()
	u.status.LastResult = msg
	u.mu.Unlock()
}

// RolledBack reports the version preflight.sh had to undo, so auto-update skips it.
func (u *Updater) RolledBack() string {
	r := u.Status().LastResult
	if v, ok := strings.CutPrefix(r, "rolled back "); ok {
		return v
	}
	return ""
}

type version struct {
	n   [3]int
	pre string
}

func parse(tag string) *version {
	s := strings.TrimPrefix(tag, "v")
	s, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil
	}
	v := &version{pre: pre}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		v.n[i] = n
	}
	return v
}

// Newer reports whether tag a is a higher semantic version than b. Development builds
// ("dev") are older than any release.
func Newer(a, b string) bool {
	va, vb := parse(a), parse(b)
	if va == nil {
		return false
	}
	if vb == nil {
		return true
	}
	for i := range 3 {
		if va.n[i] != vb.n[i] {
			return va.n[i] > vb.n[i]
		}
	}
	// 1.2.0 is newer than 1.2.0-rc.1
	switch {
	case va.pre == vb.pre:
		return false
	case va.pre == "":
		return true
	case vb.pre == "":
		return false
	}
	return comparePre(va.pre, vb.pre) > 0
}

// comparePre orders pre-release labels like semver: numeric parts numerically, so rc.10
// comes after rc.9.
func comparePre(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		switch {
		case ea == nil && eb == nil && na != nb:
			return na - nb
		case ea == nil && eb != nil:
			return -1
		case ea != nil && eb == nil:
			return 1
		case pa[i] != pb[i]:
			return strings.Compare(pa[i], pb[i])
		}
	}
	return len(pa) - len(pb)
}

// App release assets: the Android app is attached to the same GitHub releases. The
// agent serves it to paired phones, which cannot read a private repository themselves.
const (
	apkAsset     = "vps-admin.apk"
	apkMetaAsset = "vps-admin-apk.json"
)

type appCache struct {
	at   time.Time
	rel  *Release
	meta []byte
}

var (
	appMu     sync.Mutex
	appCached appCache
)

// AppLatest returns the metadata of the newest app build ({versionCode, versionName,
// sha256, size}), cached for ten minutes. ErrNoRelease when no release carries one.
func (u *Updater) AppLatest(ctx context.Context) ([]byte, *Release, error) {
	appMu.Lock()
	defer appMu.Unlock()
	if time.Since(appCached.at) < 10*time.Minute && appCached.rel != nil {
		return appCached.meta, appCached.rel, nil
	}
	rel, err := u.latestWith(ctx, false, func(r *Release) bool { return r.assets[apkAsset] != "" && r.assets[apkMetaAsset] != "" })
	if err != nil {
		return nil, nil, err
	}
	if rel == nil {
		return nil, nil, ErrNoRelease
	}
	meta, err := u.fetch(ctx, rel.assets[apkMetaAsset], 64<<10)
	if err != nil {
		return nil, nil, err
	}
	appCached = appCache{time.Now(), rel, meta}
	return meta, rel, nil
}

// AppAPK opens the APK of rel for streaming. The caller closes the body.
func (u *Updater) AppAPK(ctx context.Context, rel *Release) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rel.assets[apkAsset], nil)
	u.auth(req)
	res, err := u.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("download app: HTTP %d", res.StatusCode)
	}
	return res, nil
}
