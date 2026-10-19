// Package deploy builds GitHub repositories into Docker containers and puts them online
// behind Caddy.
//
// Single-container apps (Dockerfile, node, static) deploy without downtime: the new
// container starts on a fresh loopback port next to the old one, must answer on it, then
// Caddy switches over and the old one is removed. Images stay tagged by commit so a
// rollback restarts a previous image without rebuilding. Compose apps run with
// `docker compose up` in a folder that persists between deploys, so relative bind mounts
// keep their data.
package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beedywool/vps-admin/internal/github"
	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

const (
	maxLog        = 2 << 20
	healthTimeout = 90 * time.Second
)

var (
	AppID       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}[a-z0-9]$`)
	domainRe    = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)
	envKeyRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	ErrBusy     = errors.New("a deployment of this app is already running")
	ErrNoDomain = errors.New("domains need Caddy: the installer sets it up when Caddy is installed")
)

func ValidDomain(d string) bool { return d == "" || domainRe.MatchString(d) }
func ValidEnvKey(k string) bool { return envKeyRe.MatchString(k) }

type Engine struct {
	Store  *store.Store
	Box    *secret.Box
	GitHub *github.Client
	// DataDir holds sources (apps/, builds/); SitesDir the Caddy snippets, one per app.
	DataDir  string
	SitesDir string
	// ReloadCaddy applies the snippets; Caddy refuses a bad config and keeps the old one.
	ReloadCaddy func(context.Context) error
	// gitRemote replaces the GitHub clone URL of linked folders in tests.
	gitRemote func(repo, token string) string

	jobs chan job

	mu      sync.Mutex
	subs    map[int64]map[chan string]struct{}
	running map[string]bool
}

type job struct {
	app store.App
	dep *store.Deployment
}

func (e *Engine) Init() {
	e.jobs = make(chan job, 64)
	e.subs = map[int64]map[chan string]struct{}{}
	e.running = map[string]bool{}
}

// Run processes deployments one at a time until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	e.Store.InterruptedDeployments(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-e.jobs:
			e.process(ctx, j)
		}
	}
}

func (e *Engine) SitesEnabled() bool {
	if e.SitesDir == "" {
		return false
	}
	st, err := os.Stat(e.SitesDir)
	return err == nil && st.IsDir()
}

// Deploy queues a deployment of app at sha (the branch head when empty). image reuses a
// built image instead of building, for rollbacks.
func (e *Engine) Deploy(ctx context.Context, app store.App, trigger, sha, message, image string) (*store.Deployment, error) {
	e.mu.Lock()
	busy := e.running[app.ID]
	if !busy {
		e.running[app.ID] = true
	}
	e.mu.Unlock()
	if busy {
		return nil, ErrBusy
	}
	d := &store.Deployment{AppID: app.ID, SHA: sha, Message: message, Trigger: trigger, Status: "queued", Image: image}
	if err := e.Store.AddDeployment(ctx, d); err != nil {
		e.done(app.ID)
		return nil, err
	}
	select {
	case e.jobs <- job{app, d}:
		return d, nil
	default:
		e.done(app.ID)
		return nil, errors.New("too many deployments queued")
	}
}

func (e *Engine) done(appID string) {
	e.mu.Lock()
	delete(e.running, appID)
	e.mu.Unlock()
}

func (e *Engine) Busy(appID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running[appID]
}

// Subscribe streams log output of a running deployment.
func (e *Engine) Subscribe(id int64) (<-chan string, func()) {
	ch := make(chan string, 256)
	e.mu.Lock()
	if e.subs[id] == nil {
		e.subs[id] = map[chan string]struct{}{}
	}
	e.subs[id][ch] = struct{}{}
	e.mu.Unlock()
	return ch, func() {
		e.mu.Lock()
		delete(e.subs[id], ch)
		if len(e.subs[id]) == 0 {
			delete(e.subs, id)
		}
		e.mu.Unlock()
	}
}

// logger buffers output and writes it to the database once a second.
type logger struct {
	e    *Engine
	id   int64
	mu   sync.Mutex
	buf  bytes.Buffer
	size int
	stop chan struct{}
	wg   sync.WaitGroup
	once sync.Once
}

func (e *Engine) newLogger(id int64) *logger {
	l := &logger{e: e, id: id, stop: make(chan struct{})}
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				l.flush()
			case <-l.stop:
				l.flush()
				return
			}
		}
	}()
	return l
}

func (l *logger) Write(p []byte) (int, error) {
	l.mu.Lock()
	if l.size < maxLog {
		l.buf.Write(p)
		l.size += len(p)
		if l.size >= maxLog {
			l.buf.WriteString("\n[log truncated]\n")
		}
	}
	l.mu.Unlock()
	l.e.mu.Lock()
	for ch := range l.e.subs[l.id] {
		select {
		case ch <- string(p):
		default:
		}
	}
	l.e.mu.Unlock()
	return len(p), nil
}

func (l *logger) Printf(format string, args ...any) {
	fmt.Fprintf(l, "==> "+format+"\n", args...)
}

func (l *logger) flush() {
	l.mu.Lock()
	s := l.buf.String()
	l.buf.Reset()
	l.mu.Unlock()
	if s != "" {
		l.e.Store.AppendLog(context.Background(), l.id, s)
	}
}

func (l *logger) Close() {
	l.once.Do(func() {
		close(l.stop)
		l.wg.Wait()
	})
}

// docker runs the docker CLI, streaming its output to the log.
func (e *Engine) docker(ctx context.Context, log io.Writer, dir string, env []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	cmd.Env = append([]string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + e.DataDir, // docker keeps its config (buildx) in ~/.docker
		"DOCKER_CLI_HINTS=false",
	}, env...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.WaitDelay = 5 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s: %w", args[0], err)
	}
	return nil
}

// dockerOut runs the docker CLI and returns its output.
func (e *Engine) dockerOut(ctx context.Context, args ...string) (string, error) {
	var out bytes.Buffer
	err := e.docker(ctx, &out, "", nil, args...)
	return strings.TrimSpace(out.String()), err
}

// finish records the outcome last: the full log is stored and the app is free for a new
// deployment by the time anyone sees FinishedAt.
func (e *Engine) finish(ctx context.Context, l *logger, d *store.Deployment, err error) {
	if err != nil {
		l.Printf("FAILED: %v", err)
		d.Status, d.Error = "failed", err.Error()
	} else {
		l.Printf("Live: %s", short(d.SHA))
		d.Status = "live"
	}
	l.Close()
	e.done(d.AppID)
	now := time.Now()
	d.FinishedAt = &now
	e.Store.SaveDeployment(ctx, d)
}

func (e *Engine) setStatus(ctx context.Context, d *store.Deployment, s string) {
	d.Status = s
	e.Store.SaveDeployment(ctx, d)
}

func (e *Engine) process(ctx context.Context, j job) {
	app, d := j.app, j.dep
	l := e.newLogger(d.ID)
	defer l.Close()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	defer cancel()

	e.setStatus(ctx, d, "building")
	env, err := e.envFor(ctx, app.ID)
	if err == nil {
		if app.Dir != "" {
			err = e.updateFolder(ctx, l, app, d)
		} else if d.Image == "" || app.Build == BuildCompose {
			err = e.fetchAndBuild(ctx, l, &app, d, env)
		} else {
			l.Printf("Rolling back to %s (image %s, no rebuild)", short(d.SHA), d.Image)
			d.Build = app.Build
		}
	}
	if err == nil {
		if d.Build == BuildCompose {
			err = e.goLiveCompose(ctx, l, app, d)
		} else {
			err = e.goLiveContainer(ctx, l, app, d, env)
		}
	}
	if err == nil {
		e.prune(ctx, l, app)
	}
	e.finish(ctx, l, d, err)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// envFor returns the app variables as KEY=value, decrypted.
func (e *Engine) envFor(ctx context.Context, appID string) ([]string, error) {
	vars, err := e.Store.Env(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		val, err := e.Box.Open(v.Value)
		if err != nil {
			return nil, fmt.Errorf("variable %s cannot be decrypted: %w", v.Key, err)
		}
		out = append(out, v.Key+"="+val)
	}
	return out, nil
}

func (e *Engine) srcDir(app store.App, d *store.Deployment, build string) string {
	if build == BuildCompose {
		return filepath.Join(e.DataDir, "apps", app.ID, "src")
	}
	return filepath.Join(e.DataDir, "builds", fmt.Sprintf("%s-%d", app.ID, d.ID))
}

func (e *Engine) fetchAndBuild(ctx context.Context, l *logger, app *store.App, d *store.Deployment, env []string) error {
	if d.SHA == "" {
		c, err := e.GitHub.Commit(ctx, app.InstallationID, app.Repo, app.Branch)
		if err != nil {
			return fmt.Errorf("read %s@%s: %w", app.Repo, app.Branch, err)
		}
		d.SHA, d.Message = c.SHA, c.Message
		e.Store.SaveDeployment(ctx, d)
	}
	l.Printf("Fetching %s at %s: %s", app.Repo, short(d.SHA), d.Message)

	// Compose keeps one folder; other builds use a throwaway one.
	src := e.srcDir(*app, d, app.Build)
	if app.Build != BuildCompose {
		defer os.RemoveAll(src)
	}
	if err := os.MkdirAll(src, 0o750); err != nil {
		return err
	}
	tar, err := e.GitHub.Tarball(ctx, app.InstallationID, app.Repo, d.SHA)
	if err != nil {
		return err
	}
	err = Extract(tar, src)
	tar.Close()
	if err != nil {
		return fmt.Errorf("unpack the source: %w", err)
	}
	root, err := safeSubdir(src, app.RootDir)
	if err != nil {
		return err
	}

	files := listRoot(root)
	pkg, _ := os.ReadFile(filepath.Join(root, "package.json"))
	dfPath := "Dockerfile"
	if app.Dockerfile != "" {
		dfPath = app.Dockerfile
		files = append(files, "Dockerfile")
	}
	dockerfile, _ := os.ReadFile(filepath.Join(root, dfPath))
	det := Detect(files, string(pkg), string(dockerfile))
	build := app.Build
	if build == BuildAuto || build == "" {
		build = det.Build
		l.Printf("Detected: %s", det.Reason)
	}
	if build == "" {
		return errors.New(det.Reason)
	}
	if build == BuildCompose && app.Build != BuildCompose {
		// Auto-detected compose: move the source to the persistent folder.
		dst := e.srcDir(*app, d, BuildCompose)
		os.RemoveAll(dst)
		os.MkdirAll(filepath.Dir(dst), 0o750)
		if err := os.Rename(src, dst); err != nil {
			return err
		}
		app.Build = BuildCompose
		e.Store.UpdateApp(ctx, app)
		root, _ = safeSubdir(dst, app.RootDir)
	}
	d.Build = build
	e.Store.SaveDeployment(ctx, d)

	if build == BuildCompose {
		return e.composeUp(ctx, l, *app, root, env)
	}

	if app.Port == 0 {
		if det.Port == 0 {
			return errors.New("set the port the app listens on (no EXPOSE line in the Dockerfile)")
		}
		app.Port = det.Port
		e.Store.UpdateApp(ctx, app)
	}
	args := []string{"build", "-t", imageName(app.ID, d.SHA), "--label", "vpsadmin.app=" + app.ID}
	if build == BuildNode || build == BuildStatic {
		var p packageJSON
		json.Unmarshal(pkg, &p)
		gen := GeneratedDockerfile(build, files, p.Scripts["build"] != "", Commands{app.InstallCmd, app.BuildCmd, app.StartCmd})
		if err := os.WriteFile(filepath.Join(root, "Dockerfile.vps-admin"), []byte(gen), 0o644); err != nil {
			return err
		}
		args = append(args, "-f", "Dockerfile.vps-admin")
	} else if app.Dockerfile != "" {
		args = append(args, "-f", app.Dockerfile)
	}
	args = append(args, ".")
	l.Printf("Building image %s", imageName(app.ID, d.SHA))
	if err := e.docker(ctx, l, root, nil, args...); err != nil {
		return err
	}
	d.Image = imageName(app.ID, d.SHA)
	e.Store.SaveDeployment(ctx, d)
	return nil
}

func imageName(appID, sha string) string {
	tag := sha
	if len(tag) > 12 {
		tag = tag[:12]
	}
	return "vpsadmin/" + appID + ":" + tag
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (e *Engine) goLiveContainer(ctx context.Context, l *logger, app store.App, d *store.Deployment, env []string) error {
	e.setStatus(ctx, d, "starting")
	port, err := freePort()
	if err != nil {
		return err
	}
	name := fmt.Sprintf("vpsadmin-%s-%d", app.ID, d.ID)
	args := []string{"run", "-d", "--name", name, "--restart", "unless-stopped",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, app.Port),
		"--label", "vpsadmin.app=" + app.ID, "--label", fmt.Sprintf("vpsadmin.deployment=%d", d.ID)}
	if app.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", app.MemoryMB))
	}
	// "-e KEY" takes the value from the CLI environment: any character works and the
	// values never show up in the process list.
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		args = append(args, "-e", k)
	}
	args = append(args, d.Image)
	l.Printf("Starting %s on 127.0.0.1:%d", name, port)
	if err := e.docker(ctx, l, "", env, args...); err != nil {
		return err
	}
	d.Container, d.HostPort = name, port
	e.Store.SaveDeployment(ctx, d)

	if err := e.waitHealthy(ctx, l, name, port); err != nil {
		l.Printf("Last output of the new container:")
		e.docker(context.WithoutCancel(ctx), l, "", nil, "logs", "--tail", "60", name)
		e.docker(context.WithoutCancel(ctx), io.Discard, "", nil, "rm", "-f", name)
		return fmt.Errorf("%w; the previous version keeps running", err)
	}

	if app.Domain != "" {
		if err := e.writeSite(ctx, l, app, port); err != nil {
			e.docker(context.WithoutCancel(ctx), io.Discard, "", nil, "rm", "-f", name)
			return err
		}
	}
	return e.retirePrevious(ctx, l, app, d)
}

// waitHealthy waits until the app answers HTTP and stays up for a few seconds, so a
// crash loop is not mistaken for a healthy start. A TCP connect is not enough: Docker's
// port proxy accepts connections even when nothing listens inside the container. Any
// HTTP status counts, a 404 or 500 still means the server is up.
func (e *Engine) waitHealthy(ctx context.Context, l *logger, name string, port int) error {
	l.Printf("Waiting for the app to answer HTTP on port %d", port)
	client := &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	deadline := time.Now().Add(healthTimeout)
	var upSince time.Time
	for time.Now().Before(deadline) {
		state, err := e.dockerOut(ctx, "inspect", "-f", "{{.State.Status}} {{.RestartCount}}", name)
		if err != nil {
			return err
		}
		if f := strings.Fields(state); len(f) == 2 && (f[0] == "exited" || f[0] == "dead" || f[1] != "0") {
			return fmt.Errorf("the container stopped (%s)", state)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		res, err := client.Do(req)
		if err == nil {
			res.Body.Close()
			if upSince.IsZero() {
				upSince = time.Now()
				l.Printf("HTTP %d", res.StatusCode)
			}
			if time.Since(upSince) >= 5*time.Second {
				l.Printf("The app answers")
				return nil
			}
		} else {
			upSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("no HTTP answer on the container port within %s (is the port right?)", healthTimeout)
}

// retirePrevious stops what served the app before d and marks it superseded.
func (e *Engine) retirePrevious(ctx context.Context, l *logger, app store.App, d *store.Deployment) error {
	deps, err := e.Store.Deployments(ctx, app.ID, 200)
	if err != nil {
		return err
	}
	for i := range deps {
		old := &deps[i]
		if old.ID == d.ID || old.Status != "live" {
			continue
		}
		if old.Container != "" {
			l.Printf("Stopping the previous version (%s)", old.Container)
			e.docker(ctx, io.Discard, "", nil, "rm", "-f", old.Container)
		}
		old.Status = "superseded"
		e.Store.SaveDeployment(ctx, old)
	}
	return nil
}

func (e *Engine) composeProject(appID string) string { return "vpsadmin-" + appID }

func (e *Engine) composeUp(ctx context.Context, l *logger, app store.App, root string, env []string) error {
	// Compose reads this file for ${VAR} interpolation; variables also reach the CLI
	// environment, which compose uses the same way.
	var envFile strings.Builder
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		envFile.WriteString(k + "=" + strconv.Quote(v) + "\n")
	}
	if err := os.WriteFile(filepath.Join(root, ".env.vps-admin"), []byte(envFile.String()), 0o600); err != nil {
		return err
	}
	l.Printf("docker compose up (project %s)", e.composeProject(app.ID))
	args := []string{"compose", "-p", e.composeProject(app.ID), "--env-file", ".env.vps-admin"}
	if app.ComposeFile != "" {
		args = append(args, "-f", app.ComposeFile)
	}
	return e.docker(ctx, l, root, env, append(args, "up", "-d", "--build", "--remove-orphans")...)
}

func (e *Engine) goLiveCompose(ctx context.Context, l *logger, app store.App, d *store.Deployment) error {
	e.setStatus(ctx, d, "starting")
	l.Printf("Checking the services")
	time.Sleep(8 * time.Second)
	out, err := e.dockerOut(ctx, "compose", "-p", e.projectName(app), "ps", "-a", "--format", "{{.Service}} {{.State}} {{.ExitCode}}")
	if err != nil {
		return err
	}
	var bad []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		if f[1] == "restarting" || f[1] == "dead" || (f[1] == "exited" && f[2] != "0") {
			bad = append(bad, f[0]+" "+f[1])
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("services not running: %s", strings.Join(bad, ", "))
	}
	if app.Domain != "" {
		if app.Port == 0 {
			l.Printf("No port set: the domain is not routed. Publish a service on 127.0.0.1:PORT and set that port.")
		} else if err := e.writeSite(ctx, l, app, app.Port); err != nil {
			return err
		}
	}
	return e.retirePrevious(ctx, l, app, d)
}

func (e *Engine) sitePath(appID string) string { return filepath.Join(e.SitesDir, appID+".caddy") }

// writeSite points the app domain at port. If Caddy rejects the result (a domain already
// used elsewhere, for instance) the previous snippet is restored.
func (e *Engine) writeSite(ctx context.Context, l *logger, app store.App, port int) error {
	if !e.SitesEnabled() {
		return ErrNoDomain
	}
	path := e.sitePath(app.ID)
	prev, readErr := os.ReadFile(path)
	conf := fmt.Sprintf("# Managed by VPS Admin (app %s), rewritten on every deploy\n%s {\n\tencode zstd gzip\n\treverse_proxy 127.0.0.1:%d\n}\n", app.ID, app.Domain, port)
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		return err
	}
	l.Printf("Routing %s to 127.0.0.1:%d", app.Domain, port)
	if err := e.ReloadCaddy(ctx); err != nil {
		if readErr == nil {
			os.WriteFile(path, prev, 0o644)
		} else {
			os.Remove(path)
		}
		e.ReloadCaddy(context.WithoutCancel(ctx))
		return fmt.Errorf("Caddy refused the site for %s (is the domain already used?): %w", app.Domain, err)
	}
	return nil
}

// prune keeps the images of the last `keep` successful deployments.
func (e *Engine) prune(ctx context.Context, l *logger, app store.App) {
	if app.Build == BuildCompose {
		return
	}
	deps, err := e.Store.Deployments(ctx, app.ID, 200)
	if err != nil {
		return
	}
	keep := map[string]bool{}
	n := 0
	for _, d := range deps {
		if d.Image != "" && (d.Status == "live" || d.Status == "superseded") && !keep[d.Image] && n < max(app.Keep, 1) {
			keep[d.Image] = true
			n++
		}
	}
	out, err := e.dockerOut(ctx, "images", "vpsadmin/"+app.ID, "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return
	}
	for _, img := range strings.Split(out, "\n") {
		if img != "" && !keep[img] {
			if e.docker(ctx, io.Discard, "", nil, "rmi", img) == nil {
				l.Printf("Removed old image %s", img)
			}
		}
	}
}

// Restart restarts what currently serves the app.
func (e *Engine) Restart(ctx context.Context, app store.App) error {
	if app.Build == BuildCompose {
		return e.docker(ctx, io.Discard, "", nil, "compose", "-p", e.projectName(app), "restart")
	}
	live, err := e.Store.LiveDeployment(ctx, app.ID)
	if err != nil {
		return errors.New("the app is not running")
	}
	return e.docker(ctx, io.Discard, "", nil, "restart", live.Container)
}

// Remove deletes the containers, images, Caddy site and files of an app. Named volumes
// are only removed when volumes is true.
func (e *Engine) Remove(ctx context.Context, app store.App, volumes bool) error {
	if e.Busy(app.ID) {
		return ErrBusy
	}
	var errs []error
	src := filepath.Join(e.DataDir, "apps", app.ID, "src")
	if app.Dir != "" {
		// A linked folder belongs to the administrator: unlinking stops the updates and
		// leaves the folder and its containers exactly as they are.
	} else if app.Build == BuildCompose {
		args := []string{"compose", "-p", e.composeProject(app.ID), "down", "--remove-orphans", "--rmi", "local"}
		if volumes {
			args = append(args, "-v")
		}
		dir, err := safeSubdir(src, app.RootDir)
		if err != nil {
			dir = ""
		}
		if err := e.docker(ctx, io.Discard, dir, nil, args...); err != nil {
			errs = append(errs, err)
		}
	} else {
		ids, _ := e.dockerOut(ctx, "ps", "-aq", "--filter", "label=vpsadmin.app="+app.ID)
		if ids != "" {
			if err := e.docker(ctx, io.Discard, "", nil, append([]string{"rm", "-f"}, strings.Fields(ids)...)...); err != nil {
				errs = append(errs, err)
			}
		}
		imgs, _ := e.dockerOut(ctx, "images", "vpsadmin/"+app.ID, "--format", "{{.Repository}}:{{.Tag}}")
		for _, img := range strings.Fields(imgs) {
			e.docker(ctx, io.Discard, "", nil, "rmi", "-f", img)
		}
	}
	if _, err := os.Stat(e.sitePath(app.ID)); err == nil {
		os.Remove(e.sitePath(app.ID))
		if err := e.ReloadCaddy(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		os.RemoveAll(filepath.Join(e.DataDir, "apps", app.ID))
	}
	if err := errors.Join(errs...); err != nil {
		slog.Warn("app removal incomplete", "app", app.ID, "err", err)
		return err
	}
	return nil
}

// ContainerStatus reports the docker state of the live deployment, for the app list.
func (e *Engine) ContainerStatus(ctx context.Context, app store.App) string {
	if app.Build == BuildCompose {
		out, err := e.dockerOut(ctx, "compose", "-p", e.projectName(app), "ps", "-a", "--format", "{{.State}}")
		if err != nil || out == "" {
			return "stopped"
		}
		for _, s := range strings.Fields(out) {
			if s != "running" {
				return "degraded"
			}
		}
		return "running"
	}
	live, err := e.Store.LiveDeployment(ctx, app.ID)
	if err != nil || live.Container == "" {
		return "stopped"
	}
	out, err := e.dockerOut(ctx, "inspect", "-f", "{{.State.Status}}", live.Container)
	if err != nil {
		return "missing"
	}
	return out
}
