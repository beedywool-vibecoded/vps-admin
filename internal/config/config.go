// Package config loads the agent configuration from /etc/vps-admin/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const DefaultPath = "/etc/vps-admin/config.json"

type Config struct {
	// Listen is where the HTTP server binds. Keep it on loopback and put a reverse proxy
	// (Caddy, nginx) with TLS in front: passkeys only work on an https origin.
	Listen string `json:"listen"`
	// PublicURL is the origin the panel is reached at, e.g. https://admin.example.com.
	// It is the WebAuthn relying party, so changing it invalidates registered passkeys.
	PublicURL string `json:"public_url"`
	// WebhookURL is the public origin GitHub sends pushes to, when the panel itself is
	// private (reachable through Tailscale only, for instance). Empty means PublicURL.
	WebhookURL string `json:"webhook_url,omitempty"`
	DataDir   string `json:"data_dir"`
	// TrustedProxies are the peers allowed to set X-Forwarded-For.
	TrustedProxies []string `json:"trusted_proxies"`
	DockerSocket   string   `json:"docker_socket"`
	// CaddySitesDir receives one snippet per deployed app with a domain; the installer
	// imports it from the Caddyfile. Domains are disabled when it does not exist.
	CaddySitesDir string `json:"caddy_sites_dir"`
	// ScriptsDir holds the links made by `sudo vps-admin link`; root-owned.
	ScriptsDir string `json:"scripts_dir"`
	// Update holds the defaults; the panel stores overrides in the database.
	Update Update `json:"update"`

	prefixes []netip.Prefix
}

type Update struct {
	// Auto installs new releases on its own; when false the panel only shows them.
	Auto bool `json:"auto"`
	// Prerelease also follows GitHub pre-releases.
	Prerelease bool `json:"prerelease"`
	// GitHubToken reads releases of a private repository. A fine-grained token with
	// read-only "Contents" access to that one repository is enough.
	GitHubToken string `json:"github_token,omitempty"`
}

func Default() Config {
	return Config{
		Listen:         "127.0.0.1:7171",
		PublicURL:      "http://localhost:7171",
		DataDir:        "/var/lib/vps-admin",
		TrustedProxies: []string{"127.0.0.1/32", "::1/128"},
		DockerSocket:   "/var/run/docker.sock",
		CaddySitesDir:  "/etc/caddy/vps-admin-apps",
		ScriptsDir:     "/etc/vps-admin/scripts",
		Update:         Update{Auto: true},
	}
}

// Load reads the file at path. A missing file gives the defaults, so `vps-admin serve`
// works out of the box for local development.
func Load(path string) (*Config, error) {
	c := Default()
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func (c *Config) validate() error {
	u, err := url.Parse(c.PublicURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("public_url must be an absolute http(s) URL, got %q", c.PublicURL)
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	if c.WebhookURL != "" {
		if u, err := url.Parse(c.WebhookURL); err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("webhook_url must be an absolute https URL, got %q", c.WebhookURL)
		}
		c.WebhookURL = strings.TrimRight(c.WebhookURL, "/")
	}
	if !filepath.IsAbs(c.DataDir) {
		return fmt.Errorf("data_dir must be absolute, got %q", c.DataDir)
	}
	c.prefixes = c.prefixes[:0]
	for _, p := range c.TrustedProxies {
		pr, err := netip.ParsePrefix(p)
		if err != nil {
			return fmt.Errorf("trusted_proxies: %w", err)
		}
		c.prefixes = append(c.prefixes, pr)
	}
	return nil
}

// RPID is the WebAuthn relying party id: the host of the public URL, without port.
func (c *Config) RPID() string {
	u, _ := url.Parse(c.PublicURL)
	return u.Hostname()
}

func (c *Config) Secure() bool { return strings.HasPrefix(c.PublicURL, "https://") }

func (c *Config) IsTrustedProxy(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range c.prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
