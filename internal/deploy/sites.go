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

	"github.com/beedywool/vps-admin/internal/store"
)

// How an app is reached. Public and VPN go through the reverse proxy the installer set
// up (Caddy, or nginx with certbot); VPN only lets the listed networks in. Tailscale
// serves the app on this machine's tailnet name; local leaves it on 127.0.0.1.
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

// Printer is where a site change logs its progress: a deployment's log, or the agent's
// own log when the change comes from the Proxy tab.
type Printer interface {
	Printf(format string, args ...any)
}

// agentLog prints to the agent log, for actions with no deployment behind them.
type agentLog struct{}

func (agentLog) Printf(format string, args ...any) { slog.Info(fmt.Sprintf(format, args...)) }

// publish routes the app to its container port according to its access mode, and
// withdraws it from the ways it no longer uses.
func (e *Engine) publish(ctx context.Context, l Printer, app store.App, port int) error {
	// A URL paused from the Proxy tab stays off after every deployment: the app runs,
	// it is just not reachable.
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

// unpublish removes every route to the app (when it is deleted).
func (e *Engine) unpublish(ctx context.Context, app store.App) error {
	err := e.unpublishProxy(ctx, app)
	e.tailscaleOff(ctx, app)
	return err
}

// pausedSetting holds the apps whose URL was paused from the Proxy tab: the app keeps
// running, its domain stops answering until it is turned back on.
const pausedSetting = "proxy.paused"

// Paused tells whether an app's URL is paused.
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

// SetPaused withdraws or restores an app's URL. Its containers keep running either way:
// this is about the domain, not the app.
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

// forgetPaused drops the pause of an app that is gone, so its id can be used again.
func (e *Engine) forgetPaused(ctx context.Context, appID string) {
	m := e.pausedMap(ctx)
	if !m[appID] {
		return
	}
	delete(m, appID)
	b, _ := json.Marshal(m)
	e.Store.SetSetting(ctx, pausedSetting, string(b))
}

// LivePort is the local port the app answers on right now: the compose port it was
// given, or the port of the container that is live.
func (e *Engine) LivePort(ctx context.Context, app store.App) int {
	if app.Build != BuildCompose {
		if d, err := e.Store.LiveDeployment(ctx, app.ID); err == nil && d != nil && d.HostPort != 0 {
			return d.HostPort
		}
	}
	return app.Port
}

var snippetName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*\.(caddy|conf)$`)

// RemoveOrphan deletes a snippet this panel wrote whose app no longer exists, then
// reloads the proxy. Anything the panel did not write is refused: hand-written sites
// belong to the administrator.
func (e *Engine) RemoveOrphan(ctx context.Context, file string) error {
	path := filepath.Clean(file)
	base := filepath.Base(path)
	if !snippetName.MatchString(base) {
		return errors.New("this is not a proxy snippet")
	}
	dir := ""
	switch {
	case e.SitesDir != "" && filepath.Dir(path) == filepath.Clean(e.SitesDir):
		dir = "caddy"
	case e.NginxDir != "" && filepath.Dir(path) == filepath.Clean(e.NginxDir):
		dir = "nginx"
	default:
		return errors.New("this file is not in the directories the panel writes")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("this site is already gone")
		}
		return err
	}
	if !strings.HasPrefix(string(data), "# Managed by VPS Admin (app ") {
		return errors.New("this file was not written by the panel")
	}
	id := strings.TrimSuffix(base, filepath.Ext(base))
	if _, err := e.Store.App(ctx, id); err == nil {
		return errors.New("this snippet still serves an app: delete the app instead")
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if dir == "caddy" {
		if e.ReloadCaddy == nil {
			return nil // no Caddy to reload (tests, or a machine without it)
		}
		return e.ReloadCaddy(ctx)
	}
	if e.Helper == nil {
		return nil
	}
	_, err = e.Helper(ctx, "nginx-reload")
	return err
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

// caddySite points the domain at port; Caddy gets the certificate itself. If Caddy
// rejects the result (a domain already used elsewhere, for instance) the previous
// snippet is restored.
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

// nginxSite writes the app's server block, then asks certbot for HTTPS. The block is
// rewritten on every deploy; certbot reinstalls the existing certificate into it.
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
		// The app stays reachable over HTTP; HTTPS comes at the next deploy once DNS is right.
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

// tailscaleServe publishes the app at https://<machine>.<tailnet>.ts.net:<port>, only on
// the tailnet. Each app keeps its own port, picked once from 8443 up.
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

// writeShared writes a proxy site readable by the proxy, which runs as another user:
// the agent's own umask (0077) would otherwise make it private.
func writeShared(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}
