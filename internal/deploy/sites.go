package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/beedywool/vps-admin/internal/proxy"
	"github.com/beedywool/vps-admin/internal/store"
)

// Access modes. Public and VPN go through the reverse proxy; VPN only lets the listed
// networks in.
const (
	AccessPublic    = "public"
	AccessVPN       = "vpn"
	AccessTailscale = "tailscale"
	AccessLocal     = "local"
)

// Access is the app's mode; apps from before access modes are public with a domain.
func Access(app store.App) string {
	if app.Access != "" {
		return app.Access
	}
	if app.Domain != "" {
		return AccessPublic
	}
	return AccessLocal
}

// Proxy names the reverse proxy apps are published through: caddy, nginx or "".
func (e *Engine) Proxy() string {
	isDir := func(p string) bool {
		st, err := os.Stat(p)
		return p != "" && err == nil && st.IsDir()
	}
	switch {
	case isDir(e.SitesDir):
		return "caddy"
	case isDir(e.NginxDir) && e.Helper != nil:
		return "nginx"
	}
	return ""
}

// HasHelper tells whether the root proxy helper is installed.
func (e *Engine) HasHelper() bool { return e.Helper != nil }

// SitesEnabled tells whether apps can have a domain.
func (e *Engine) SitesEnabled() bool { return e.Proxy() != "" }

// AllowList parses the networks of a VPN-only app.
func AllowList(s string) ([]string, error) {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if _, n, err := net.ParseCIDR(f); err == nil {
			out = append(out, n.String())
		} else if ip := net.ParseIP(f); ip != nil {
			out = append(out, ip.String())
		} else {
			return nil, fmt.Errorf("%q is not a network (like 10.8.0.0/24) or an address", f)
		}
	}
	return out, nil
}

// URL is where the app answers, for the panel.
func (e *Engine) URL(ctx context.Context, app store.App) string {
	switch Access(app) {
	case AccessTailscale:
		if app.TSPort != 0 && e.TailscaleName != nil {
			if name := e.TailscaleName(ctx); name != "" {
				return fmt.Sprintf("https://%s:%d", name, app.TSPort)
			}
		}
	case AccessPublic, AccessVPN:
		if app.Domain != "" {
			return "https://" + app.Domain
		}
	}
	return ""
}

// Printer receives a site change's progress (deployment log or agent log).
type Printer interface {
	Printf(format string, args ...any)
}

type agentLog struct{}

func (agentLog) Printf(format string, args ...any) { slog.Info(fmt.Sprintf(format, args...)) }

// publish routes the app according to its access mode and withdraws the other routes.
func (e *Engine) publish(ctx context.Context, l Printer, app store.App, port int) error {
	// A URL paused from the Proxy tab stays off across deployments.
	if e.Paused(ctx, app.ID) {
		err := e.unpublishProxy(ctx, app)
		e.tailscaleOff(ctx, app)
		if app.Domain != "" {
			l.Printf("URL of %s paused: turn it back on from the Proxy tab", app.Domain)
		}
		return err
	}
	switch Access(app) {
	case AccessLocal:
		e.unpublishProxy(ctx, app)
		e.tailscaleOff(ctx, app)
		l.Printf("No domain: the app answers on 127.0.0.1:%d only", port)
		return nil
	case AccessTailscale:
		e.unpublishProxy(ctx, app)
		return e.tailscaleServe(ctx, l, app, port)
	default:
		e.tailscaleOff(ctx, app)
		if app.Domain == "" {
			return nil
		}
		switch e.Proxy() {
		case "caddy":
			return e.caddySite(ctx, l, app, port)
		case "nginx":
			return e.nginxSite(ctx, l, app, port)
		}
		return ErrNoDomain
	}
}

// unpublish removes every route to the app.
func (e *Engine) unpublish(ctx context.Context, app store.App) error {
	err := e.unpublishProxy(ctx, app)
	e.tailscaleOff(ctx, app)
	return err
}

// pausedSetting holds the apps whose URL was paused; the app itself keeps running.
const pausedSetting = "proxy.paused"

func (e *Engine) Paused(ctx context.Context, appID string) bool {
	return e.pausedMap(ctx)[appID]
}

func (e *Engine) pausedMap(ctx context.Context) map[string]bool {
	m := map[string]bool{}
	if e.Store == nil {
		return m
	}
	json.Unmarshal([]byte(e.Store.Setting(ctx, pausedSetting, "{}")), &m)
	return m
}

// SetPaused withdraws or restores an app's URL. Containers keep running either way.
func (e *Engine) SetPaused(ctx context.Context, app store.App, paused bool) error {
	m := e.pausedMap(ctx)
	if paused {
		m[app.ID] = true
	} else {
		delete(m, app.ID)
	}
	b, _ := json.Marshal(m)
	if err := e.Store.SetSetting(ctx, pausedSetting, string(b)); err != nil {
		return err
	}
	if paused {
		return e.unpublish(ctx, app)
	}
	port := e.LivePort(ctx, app)
	if port == 0 {
		return errors.New("this app has no port to publish: deploy it once first")
	}
	return e.publish(ctx, agentLog{}, app, port)
}

// forgetPaused drops the pause of a deleted app so its id can be reused.
func (e *Engine) forgetPaused(ctx context.Context, appID string) {
	m := e.pausedMap(ctx)
	if !m[appID] {
		return
	}
	delete(m, appID)
	b, _ := json.Marshal(m)
	e.Store.SetSetting(ctx, pausedSetting, string(b))
}

// LivePort is the compose port, or the port of the live container.
func (e *Engine) LivePort(ctx context.Context, app store.App) int {
	if app.Build != BuildCompose {
		if d, err := e.Store.LiveDeployment(ctx, app.ID); err == nil && d != nil && d.HostPort != 0 {
			return d.HostPort
		}
	}
	return app.Port
}

var snippetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*\.(caddy|conf)$`)

// PausedSite is a site file taken out of the proxy and kept aside.
type PausedSite struct {
	File   string `json:"file"`   // where it belongs, and the path the panel sends back
	Stored string `json:"stored"` // the kept copy, under the data directory
	Engine string `json:"engine"` // caddy or nginx
}

// siteFile only accepts a site directly inside one of the proxy's directories, or a Caddy
// file the proxy imports. Anything else belongs to the admin.
func (e *Engine) siteFile(file string) (path, engine string, err error) {
	path = filepath.Clean(file)
	base := filepath.Base(path)
	switch {
	case e.SitesDir != "" && snippetName.MatchString(base) && filepath.Dir(path) == filepath.Clean(e.SitesDir):
		return path, "caddy", nil
	case e.NginxDir != "" && snippetName.MatchString(base) && filepath.Dir(path) == filepath.Clean(e.NginxDir):
		return path, "nginx", nil
	case e.SitesDir != "" && sharedCaddyFile(path, filepath.Dir(filepath.Clean(e.SitesDir))):
		return path, "caddy", nil
	}
	return "", "", errors.New("this is not a proxy site file")
}

// sharedCaddyFile reports the main Caddyfile and the files it imports. Several sites can
// share one, so edits target a block by domain.
func sharedCaddyFile(path, root string) bool {
	if root == "" || strings.Contains(strings.ToLower(filepath.Base(path)), ".bak") {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if path == filepath.Join(root, "Caddyfile") {
		return true
	}
	return strings.HasSuffix(filepath.Base(path), ".caddy")
}

func (e *Engine) sharedCaddy(path string) bool {
	return e.SitesDir != "" && filepath.Dir(filepath.Clean(path)) != filepath.Clean(e.SitesDir)
}

// siteOwner is the app a site file belongs to, if that app still exists.
func (e *Engine) siteOwner(ctx context.Context, path string) *store.App {
	// Only the panel's snippets are named after an app.
	if e.Store == nil {
		return nil
	}
	dir := filepath.Dir(filepath.Clean(path))
	inSites := e.SitesDir != "" && dir == filepath.Clean(e.SitesDir)
	inNginx := e.NginxDir != "" && dir == filepath.Clean(e.NginxDir)
	if !inSites && !inNginx {
		return nil
	}
	base := filepath.Base(path)
	app, err := e.Store.App(ctx, strings.TrimSuffix(base, filepath.Ext(base)))
	if err != nil {
		return nil
	}
	return app
}

// reloadProxy reloads Caddy directly, nginx through the root helper.
func (e *Engine) reloadProxy(ctx context.Context, engine string) error {
	if engine == "nginx" {
		if e.Helper == nil {
			return nil
		}
		if out, err := e.Helper(ctx, "nginx-reload"); err != nil {
			return fmt.Errorf("nginx refused the change: %s", lastLine(out, err))
		}
		return nil
	}
	if e.ReloadCaddy == nil {
		return nil
	}
	return e.ReloadCaddy(ctx)
}

func (e *Engine) pausedDir() string { return filepath.Join(e.DataDir, "proxy-paused") }

// storedName keeps the engine in the name so a resumed site goes back to the right place.
func storedName(engine, base string) string { return engine + "__" + base }

func splitStored(name string) (engine, base string, ok bool) {
	engine, base, ok = strings.Cut(name, "__")
	if !ok || base == "" || !snippetName.MatchString(base) {
		return "", "", false
	}
	return engine, base, true
}

// PauseSite moves a site file aside and reloads the proxy.
func (e *Engine) PauseSite(ctx context.Context, file, domain string) error {
	path, engine, err := e.siteFile(file)
	if err != nil {
		return err
	}
	if engine == "caddy" && e.sharedCaddy(path) {
		return e.pauseSharedSite(ctx, path, domain)
	}
	base := filepath.Base(path)
	if app := e.siteOwner(ctx, path); app != nil {
		return fmt.Errorf("%s is the site of app %s: pause that app instead", base, app.ID)
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return errors.New("this site is already gone")
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.pausedDir(), 0o700); err != nil {
		return err
	}
	stored := filepath.Join(e.pausedDir(), storedName(engine, base))
	if err := os.WriteFile(stored, data, 0o644); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		os.Remove(stored)
		return err
	}
	if err := e.reloadProxy(ctx, engine); err != nil {
		// Reload failed: put the site back.
		writeShared(path, data)
		os.Remove(stored)
		e.reloadProxy(context.WithoutCancel(ctx), engine)
		return err
	}
	return nil
}

// ResumeSite restores a paused site byte for byte and reloads the proxy.
func (e *Engine) ResumeSite(ctx context.Context, file, domain string) error {
	path, engine, err := e.siteFile(file)
	if err != nil {
		return err
	}
	if engine == "caddy" && e.sharedCaddy(path) {
		return e.resumeSharedSite(ctx, path, domain)
	}
	stored := filepath.Join(e.pausedDir(), storedName(engine, filepath.Base(path)))
	data, err := os.ReadFile(stored)
	if os.IsNotExist(err) {
		return errors.New("this site is not paused")
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return errors.New("a site with this name exists again: delete it first")
	}
	if err := writeShared(path, data); err != nil {
		return err
	}
	if err := e.reloadProxy(ctx, engine); err != nil {
		os.Remove(path)
		e.reloadProxy(context.WithoutCancel(ctx), engine)
		return err
	}
	os.Remove(stored)
	return nil
}

// DeleteSite removes a site and its paused copy. Sites still served by an app are
// refused, since the next deploy would rewrite them.
func (e *Engine) DeleteSite(ctx context.Context, file, domain string) error {
	path, engine, err := e.siteFile(file)
	if err != nil {
		return err
	}
	if engine == "caddy" && e.sharedCaddy(path) {
		return e.deleteSharedSite(ctx, path, domain)
	}
	base := filepath.Base(path)
	if app := e.siteOwner(ctx, path); app != nil {
		return fmt.Errorf("%s serves app %s: delete that app, or pause its URL", base, app.ID)
	}
	removed := false
	if err := os.Remove(path); err == nil {
		removed = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(filepath.Join(e.pausedDir(), storedName(engine, base))); err == nil {
		removed = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if !removed {
		return errors.New("this site is already gone")
	}
	return e.reloadProxy(ctx, engine)
}

// pauseSharedSite comments out one site block, leaving the rest of the file alone.
func (e *Engine) pauseSharedSite(ctx context.Context, path, domain string) error {
	if strings.TrimSpace(domain) == "" {
		return errors.New("this file holds several sites: say which domain to pause")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return errors.New("this site is already gone")
	}
	if err != nil {
		return err
	}
	next, ok := proxy.CommentBlock(string(data), domain)
	if !ok {
		return fmt.Errorf("no site for %s in %s: it may already be paused, or written elsewhere", domain, filepath.Base(path))
	}
	return e.applySharedSite(ctx, path, data, []byte(next))
}

func (e *Engine) resumeSharedSite(ctx context.Context, path, domain string) error {
	if strings.TrimSpace(domain) == "" {
		return errors.New("this file holds several sites: say which domain to resume")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, ok := proxy.RestoreBlock(string(data), domain)
	if !ok {
		return errors.New("this site is not paused")
	}
	return e.applySharedSite(ctx, path, data, []byte(next))
}

func (e *Engine) deleteSharedSite(ctx context.Context, path, domain string) error {
	if strings.TrimSpace(domain) == "" {
		return errors.New("this file holds several sites: say which domain to delete")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, ok := proxy.RemoveBlock(string(data), domain)
	if !ok {
		return errors.New("this site is already gone")
	}
	return e.applySharedSite(ctx, path, data, []byte(next))
}

// applySharedSite writes a Caddyfile and reloads Caddy, restoring the previous file if
// the config is refused.
func (e *Engine) applySharedSite(ctx context.Context, path string, prev, next []byte) error {
	if err := e.writeCaddy(ctx, path, next); err != nil {
		return err
	}
	if err := e.reloadProxy(ctx, "caddy"); err != nil {
		if werr := e.writeCaddy(context.WithoutCancel(ctx), path, prev); werr != nil {
			slog.Error("cannot put the Caddy configuration back", "file", path, "err", werr)
		}
		e.reloadProxy(context.WithoutCancel(ctx), "caddy")
		return err
	}
	return nil
}

// writeCaddy goes through the helper since these files belong to root; tests write them directly.
func (e *Engine) writeCaddy(ctx context.Context, path string, data []byte) error {
	if e.WriteCaddy != nil {
		return e.WriteCaddy(ctx, path, data)
	}
	return writeShared(path, data)
}

// EnableHTTPS runs certbot on an nginx site. Caddy handles certificates itself.
func (e *Engine) EnableHTTPS(ctx context.Context, file, domain string) error {
	path, engine, err := e.siteFile(file)
	if err != nil {
		return err
	}
	if engine != "nginx" {
		return errors.New("Caddy issues its own certificates")
	}
	if e.Helper == nil {
		return errors.New("the proxy helper is not installed on this server")
	}
	if _, err := os.Stat(path); err != nil {
		return errors.New("this site is paused: turn it back on first")
	}
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" || strings.ContainsAny(d, " \t\n/") {
		return errors.New("a domain name is expected")
	}
	if out, err := e.Helper(ctx, "certbot", d); err != nil {
		return fmt.Errorf("certbot could not get a certificate for %s: %s", d, lastLine(out, err))
	}
	return nil
}

func (e *Engine) PausedSites() []PausedSite {
	entries, err := os.ReadDir(e.pausedDir())
	if err != nil {
		return []PausedSite{}
	}
	out := []PausedSite{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		engine, base, ok := splitStored(entry.Name())
		if !ok {
			continue
		}
		dir := e.SitesDir
		if engine == "nginx" {
			dir = e.NginxDir
		}
		if dir == "" {
			continue
		}
		out = append(out, PausedSite{
			File:   filepath.Join(dir, base),
			Stored: filepath.Join(e.pausedDir(), entry.Name()),
			Engine: engine,
		})
	}
	return out
}

func (e *Engine) caddyPath(id string) string { return filepath.Join(e.SitesDir, id+".caddy") }
func (e *Engine) nginxPath(id string) string { return filepath.Join(e.NginxDir, id+".conf") }

func (e *Engine) unpublishProxy(ctx context.Context, app store.App) error {
	var errs []error
	if e.SitesDir != "" {
		if _, err := os.Stat(e.caddyPath(app.ID)); err == nil {
			os.Remove(e.caddyPath(app.ID))
			errs = append(errs, e.ReloadCaddy(ctx))
		}
	}
	if e.NginxDir != "" && e.Helper != nil {
		if _, err := os.Stat(e.nginxPath(app.ID)); err == nil {
			os.Remove(e.nginxPath(app.ID))
			_, err := e.Helper(ctx, "nginx-reload")
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// caddySite points the domain at port. If Caddy rejects the result the previous snippet
// is restored.
func (e *Engine) caddySite(ctx context.Context, l Printer, app store.App, port int) error {
	path := e.caddyPath(app.ID)
	prev, readErr := os.ReadFile(path)
	var b strings.Builder
	fmt.Fprintf(&b, "# Managed by VPS Admin (app %s), rewritten on every deploy\n%s {\n\tencode zstd gzip\n", app.ID, app.Domain)
	if Access(app) == AccessVPN {
		nets, _ := AllowList(app.Allow)
		fmt.Fprintf(&b, "\t@outside not remote_ip %s\n\trespond @outside 403\n", strings.Join(nets, " "))
	}
	fmt.Fprintf(&b, "\treverse_proxy 127.0.0.1:%d\n}\n", port)
	if err := writeShared(path, []byte(b.String())); err != nil {
		return err
	}
	l.Printf("Routing %s to 127.0.0.1:%d (Caddy%s)", app.Domain, port, vpnNote(app))
	if err := e.ReloadCaddy(ctx); err != nil {
		if readErr == nil {
			writeShared(path, prev)
		} else {
			os.Remove(path)
		}
		e.ReloadCaddy(context.WithoutCancel(ctx))
		return fmt.Errorf("Caddy refused the site for %s (is the domain already used?): %w", app.Domain, err)
	}
	return nil
}

// nginxSite writes the server block on every deploy; certbot reinstalls the existing
// certificate into it.
func (e *Engine) nginxSite(ctx context.Context, l Printer, app store.App, port int) error {
	path := e.nginxPath(app.ID)
	prev, readErr := os.ReadFile(path)
	allow := ""
	if Access(app) == AccessVPN {
		nets, _ := AllowList(app.Allow)
		for _, n := range nets {
			allow += "\t\tallow " + n + ";\n"
		}
		allow += "\t\tdeny all;\n"
	}
	conf := fmt.Sprintf(`# Managed by VPS Admin (app %s), rewritten on every deploy
server {
	listen 80;
	listen [::]:80;
	server_name %s;
	client_max_body_size 100m;

	location / {
%s		proxy_pass http://127.0.0.1:%d;
		proxy_http_version 1.1;
		proxy_set_header Host $host;
		proxy_set_header X-Real-IP $remote_addr;
		proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto $scheme;
		proxy_set_header Upgrade $http_upgrade;
		proxy_set_header Connection $http_connection;
		proxy_read_timeout 1h;
	}
}
`, app.ID, app.Domain, allow, port)
	if err := writeShared(path, []byte(conf)); err != nil {
		return err
	}
	l.Printf("Routing %s to 127.0.0.1:%d (nginx%s)", app.Domain, port, vpnNote(app))
	if out, err := e.Helper(ctx, "nginx-reload"); err != nil {
		if readErr == nil {
			writeShared(path, prev)
		} else {
			os.Remove(path)
		}
		e.Helper(context.WithoutCancel(ctx), "nginx-reload")
		return fmt.Errorf("nginx refused the site for %s (is the domain already used?): %s", app.Domain, lastLine(out, err))
	}
	l.Printf("Setting up HTTPS for %s with certbot", app.Domain)
	if out, err := e.Helper(ctx, "certbot", app.Domain); err != nil {
		l.Printf("HTTPS not set up (DNS of %s must point here and port 80 be open): %s", app.Domain, lastLine(out, err))
	}
	return nil
}

func vpnNote(app store.App) string {
	if Access(app) == AccessVPN {
		return ", VPN networks only"
	}
	return ""
}

func lastLine(out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return l
	}
	return err.Error()
}

// tailscaleServe publishes the app on the tailnet only, each app on its own port from 8443 up.
func (e *Engine) tailscaleServe(ctx context.Context, l Printer, app store.App, port int) error {
	if e.Helper == nil || e.TailscaleName == nil || e.TailscaleName(ctx) == "" {
		return errors.New("Tailscale is not running on this server")
	}
	if app.TSPort == 0 {
		used := map[int]bool{443: true, 10443: true} // the panel's own tailnet ports
		if apps, err := e.Store.Apps(ctx); err == nil {
			for _, a := range apps {
				used[a.TSPort] = true
			}
		}
		p := 8443
		for used[p] {
			p++
		}
		app.TSPort = p
		if err := e.Store.UpdateApp(ctx, &app); err != nil {
			return err
		}
	}
	if out, err := e.Helper(ctx, "ts-serve", fmt.Sprint(app.TSPort), fmt.Sprint(port)); err != nil {
		return fmt.Errorf("tailscale serve failed (HTTPS certificates must be enabled in the tailnet): %s", lastLine(out, err))
	}
	l.Printf("Serving on %s (Tailscale only)", e.URL(ctx, app))
	return nil
}

func (e *Engine) tailscaleOff(ctx context.Context, app store.App) {
	if app.TSPort != 0 && e.Helper != nil {
		e.Helper(ctx, "ts-off", fmt.Sprint(app.TSPort))
	}
}

// writeShared makes the file readable by the proxy user; the agent's umask is 0077.
func writeShared(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}
