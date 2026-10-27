package system

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Environment is what the server offers for publishing apps, used to offer the right
// access modes.
type Environment struct {
	Caddy     Tool       `json:"caddy"`
	Nginx     Tool       `json:"nginx"`
	Certbot   Tool       `json:"certbot"` // NginxPlugin tells whether it can configure nginx
	Traefik   bool       `json:"traefik"` // running in a container, managed outside the panel
	Tailscale *Tailscale `json:"tailscale,omitempty"`
	WireGuard []VPNLink  `json:"wireguard"`
	ZeroTier  []VPNLink  `json:"zerotier"`
	UFW       string     `json:"ufw"` // active, inactive, or empty when absent
	Nftables  bool       `json:"nftables"`
	Docker    string     `json:"docker"` // server version
	Checked   time.Time  `json:"checked"`
}

type Tool struct {
	Installed   bool   `json:"installed"`
	Running     bool   `json:"running"`
	Version     string `json:"version,omitempty"`
	NginxPlugin bool   `json:"nginxPlugin,omitempty"`
}

type Tailscale struct {
	Running bool     `json:"running"`
	Name    string   `json:"name"`    // MagicDNS name, like machine.tailnet.ts.net
	IPs     []string `json:"ips"`     // 100.x and fd7a: addresses
	HTTPS   bool     `json:"https"`   // the tailnet issues certificates (serve can use HTTPS)
	Version string   `json:"version"` // client version
}

// VPNLink is a private network interface and its addresses (CIDR).
type VPNLink struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
}

var (
	envMu    sync.Mutex
	envCache *Environment
)

// DetectEnvironment looks at the server, at most once a minute.
func (r *Runner) DetectEnvironment(ctx context.Context) *Environment {
	envMu.Lock()
	defer envMu.Unlock()
	if envCache != nil && time.Since(envCache.Checked) < time.Minute {
		return envCache
	}
	e := &Environment{WireGuard: []VPNLink{}, ZeroTier: []VPNLink{}, Checked: time.Now()}
	run := func(argv ...string) (string, bool) {
		if _, err := exec.LookPath(argv[0]); err != nil {
			return "", false
		}
		out, err := r.Run(ctx, 10*time.Second, argv...)
		return strings.TrimSpace(out), err == nil
	}
	active := func(unit string) bool {
		out, _ := run("systemctl", "is-active", unit)
		return out == "active"
	}
	if v, ok := run("caddy", "version"); ok {
		e.Caddy = Tool{Installed: true, Running: active("caddy"), Version: toolVersion(v)}
	}
	if v, _ := run("nginx", "-v"); v != "" {
		e.Nginx = Tool{Installed: true, Running: active("nginx"), Version: toolVersion(v)}
	}
	if v, ok := run("certbot", "--version"); ok {
		e.Certbot = Tool{Installed: true, Version: toolVersion(v)}
		e.Certbot.Running = active("certbot.timer")
		_, err := os.Stat("/usr/lib/python3/dist-packages/certbot_nginx")
		e.Certbot.NginxPlugin = err == nil || hasPluginDir()
	}
	if v, ok := run("docker", "version", "--format", "{{.Server.Version}}"); ok {
		e.Docker = v
	}
	if out, ok := run("docker", "ps", "--format", "{{.Image}}"); ok {
		e.Traefik = strings.Contains(out, "traefik")
	}
	if _, err := exec.LookPath("tailscale"); err == nil {
		e.Tailscale = tailscaleStatus(ctx, r)
	}
	e.WireGuard, e.ZeroTier = vpnInterfaces(ctx, r)
	if out, err := r.Sudo(ctx, 10*time.Second, "ufw", "status"); err == nil {
		if strings.Contains(out, "Status: active") {
			e.UFW = "active"
		} else {
			e.UFW = "inactive"
		}
	}
	_, err := os.Stat("/etc/nftables.conf")
	e.Nftables = err == nil && active("nftables")
	envCache = e
	return e
}

// versionIn matches a release number inside a tool's output.
var versionIn = regexp.MustCompile(`\bv?\d+\.\d+(?:\.\d+)*\b`)

// toolVersion finds the release number in a tool's output: caddy mixes in warnings and
// nginx prefixes it.
func toolVersion(out string) string {
	var first string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "{") || strings.HasPrefix(line, "[") {
			continue // a log record, not the answer to the command
		}
		if v := versionIn.FindString(line); v != "" {
			return v
		}
		if first == "" {
			first = line
		}
	}
	return first
}

// certbot's nginx plugin also comes from pip or snap.
func hasPluginDir() bool {
	for _, p := range []string{"/snap/certbot/current/lib/python3.12/site-packages/certbot_nginx", "/opt/certbot"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func tailscaleStatus(ctx context.Context, r *Runner) *Tailscale {
	t := &Tailscale{IPs: []string{}}
	if v, err := r.Run(ctx, 5*time.Second, "tailscale", "version"); err == nil {
		t.Version, _, _ = strings.Cut(strings.TrimSpace(v), "\n")
	}
	out, err := r.Run(ctx, 10*time.Second, "tailscale", "status", "--json")
	if err != nil {
		return t
	}
	var st struct {
		BackendState string
		Self         struct {
			DNSName      string
			TailscaleIPs []string
		}
		CertDomains []string
	}
	if json.Unmarshal([]byte(out), &st) != nil {
		return t
	}
	t.Running = st.BackendState == "Running"
	t.Name = strings.TrimSuffix(st.Self.DNSName, ".")
	t.IPs = append(t.IPs, st.Self.TailscaleIPs...)
	t.HTTPS = len(st.CertDomains) > 0
	return t
}

var vpnIface = regexp.MustCompile(`^(wg|zt)`)

func vpnInterfaces(ctx context.Context, r *Runner) (wg, zt []VPNLink) {
	wg, zt = []VPNLink{}, []VPNLink{}
	out, err := r.Run(ctx, 5*time.Second, "ip", "-j", "addr")
	if err != nil {
		return
	}
	var ifs []struct {
		Ifname   string `json:"ifname"`
		LinkType string `json:"link_type"`
		AddrInfo []struct {
			Local     string `json:"local"`
			Prefixlen int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if json.Unmarshal([]byte(out), &ifs) != nil {
		return
	}
	for _, i := range ifs {
		if !vpnIface.MatchString(i.Ifname) {
			continue
		}
		l := VPNLink{Name: i.Ifname, Addresses: []string{}}
		for _, a := range i.AddrInfo {
			l.Addresses = append(l.Addresses, a.Local+"/"+strconv.Itoa(a.Prefixlen))
		}
		if strings.HasPrefix(i.Ifname, "wg") {
			wg = append(wg, l)
		} else {
			zt = append(zt, l)
		}
	}
	return
}
