package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write creates a file under root, making the directories it needs.
func write(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// find returns the site whose first domain is want.
func find(t *testing.T, rep Report, want string) Site {
	t.Helper()
	for _, s := range rep.Sites {
		if len(s.Domains) > 0 && s.Domains[0] == want {
			return s
		}
	}
	t.Fatalf("no site for %s in %+v", want, rep.Sites)
	return Site{}
}

func TestCaddySites(t *testing.T) {
	root := t.TempDir()
	write(t, root, "etc/caddy/Caddyfile", `{
	email admin@example.com
}

import conf.d/*.caddy
import vps-admin-apps/*.caddy
`)
	write(t, root, "etc/caddy/conf.d/hand.caddy", `hand.example.com {
	reverse_proxy 127.0.0.1:9000
}
`)
	// The snippet the panel writes for a VPN-only app.
	write(t, root, "etc/caddy/vps-admin-apps/blog.caddy", `# Managed by VPS Admin (app blog), rewritten on every deploy
blog.example.com {
	encode zstd gzip
	@outside not remote_ip 10.8.0.0/24
	respond @outside 403
	reverse_proxy 127.0.0.1:8080
}
`)
	write(t, root, "etc/caddy/vps-admin-apps/files.caddy", `# Managed by VPS Admin (app files), rewritten on every deploy
files.example.com {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8081
}
`)
	write(t, root, "etc/caddy/vps-admin-apps/static.caddy", `files.example.com {
	root * /var/www
	file_server
}
`)

	rep := Read(filepath.Join(root, "etc/caddy/vps-admin-apps"), filepath.Join(root, "etc/nginx/vps-admin-apps"))
	if rep.Engine != "caddy" {
		t.Fatalf("engine = %q, want caddy", rep.Engine)
	}
	if len(rep.Sites) != 4 {
		t.Fatalf("got %d sites, want 4: %+v", len(rep.Sites), rep.Sites)
	}

	blog := find(t, rep, "blog.example.com")
	if blog.Target != "127.0.0.1:8080" || !blog.Managed || blog.ID != "blog" || !blog.VPNOnly || blog.Scheme != "https" {
		t.Fatalf("blog site = %+v", blog)
	}
	if want := filepath.Join(root, "etc/caddy/vps-admin-apps/blog.caddy"); blog.File != want {
		t.Fatalf("blog file = %q, want %q", blog.File, want)
	}

	hand := find(t, rep, "hand.example.com")
	if hand.Target != "127.0.0.1:9000" || hand.Managed || hand.ID != "" || hand.VPNOnly || hand.Scheme != "https" {
		t.Fatalf("hand site = %+v", hand)
	}
}

func TestCaddyAddressesAndImports(t *testing.T) {
	root := t.TempDir()
	write(t, root, "etc/caddy/Caddyfile", `import sites/*.caddy
`)
	write(t, root, "etc/caddy/sites/multi.caddy", `www.example.com, example.com {
	reverse_proxy localhost:3000
}

http://plain.example.com {
	reverse_proxy 127.0.0.1:4000
}
`)

	rep := Read(filepath.Join(root, "etc/caddy/vps-admin-apps"), "")
	if len(rep.Sites) != 2 {
		t.Fatalf("got %d sites, want 2: %+v", len(rep.Sites), rep.Sites)
	}
	multi := find(t, rep, "www.example.com")
	if len(multi.Domains) != 2 || multi.Domains[1] != "example.com" || multi.Target != "localhost:3000" {
		t.Fatalf("multi site = %+v", multi)
	}
	plain := find(t, rep, "plain.example.com")
	if plain.Scheme != "http" || plain.Target != "127.0.0.1:4000" {
		t.Fatalf("plain site = %+v", plain)
	}
}

func TestNginxSites(t *testing.T) {
	root := t.TempDir()
	write(t, root, "etc/nginx/nginx.conf", `http {
	upstream app_bank {
		server 127.0.0.1:7000;
	}
}
`)
	write(t, root, "etc/nginx/sites-enabled/default", `server {
	listen 80 default_server;
	server_name _;
	return 301 https://$host$request_uri;
}
`)
	write(t, root, "etc/nginx/conf.d/manual.conf", `server {
	listen 443 ssl;
	server_name manual.example.com;
	ssl_certificate /etc/letsencrypt/live/manual.example.com/fullchain.pem;
	location / {
		proxy_pass http://app_bank;
	}
}
`)
	// The snippet the panel writes for an app, the copy certbot leaves behind, and the
	// backup a package upgrade writes: only the first is a site.
	write(t, root, "etc/nginx/vps-admin-apps/shop.conf.bak", `server {
	listen 80;
	server_name old-shop.example.com;
}
`)
	// The marker is not always at the end of the name, and not always lower case.
	write(t, root, "etc/nginx/vps-admin-apps/legacy.bak.conf", `server {
	listen 80;
	server_name legacy-shop.example.com;
}
`)
	write(t, root, "etc/nginx/conf.d/archive.conf.BAK", `server {
	listen 80;
	server_name archived.example.com;
}
`)
	write(t, root, "etc/nginx/vps-admin-apps/shop.conf", `# Managed by VPS Admin (app shop), rewritten on every deploy
server {
	listen 80;
	listen [::]:80;
	server_name shop.example.com;
	client_max_body_size 100m;

	location / {
		proxy_pass http://127.0.0.1:8080;
		proxy_set_header Host $host;
	}
}
`)
	write(t, root, "etc/nginx/vps-admin-apps/private.conf", `server {
	listen 80;
	server_name private.example.com;

	location / {
		allow 10.8.0.0/24;
		deny all;
		proxy_pass http://127.0.0.1:8082;
	}
}
`)

	rep := Read("", filepath.Join(root, "etc/nginx/vps-admin-apps"))
	if rep.Engine != "nginx" {
		t.Fatalf("engine = %q, want nginx", rep.Engine)
	}
	if len(rep.Sites) != 3 {
		t.Fatalf("got %d sites, want 3 (the catch-all and the .bak copies drop out): %+v", len(rep.Sites), rep.Sites)
	}
	for _, s := range rep.Sites {
		if strings.Contains(strings.ToLower(s.File), ".bak") {
			t.Fatalf("a backup file is listed as a site: %+v", s)
		}
	}

	shop := find(t, rep, "shop.example.com")
	if shop.Target != "127.0.0.1:8080" || !shop.Managed || shop.ID != "shop" || shop.Scheme != "http" {
		t.Fatalf("shop site = %+v", shop)
	}
	manual := find(t, rep, "manual.example.com")
	if manual.Target != "127.0.0.1:7000" || manual.Managed || manual.Scheme != "https" {
		t.Fatalf("manual site = %+v (the upstream name should resolve)", manual)
	}
	private := find(t, rep, "private.example.com")
	if !private.VPNOnly || private.Target != "127.0.0.1:8082" {
		t.Fatalf("private site = %+v", private)
	}
}

func TestNginxPlainHTTPRedirectIsDropped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "etc/nginx/conf.d/site.conf", `server {
	listen 443 ssl;
	server_name site.example.com;
	ssl_certificate /etc/letsencrypt/live/site.example.com/fullchain.pem;
	location / {
		proxy_pass http://127.0.0.1:3000;
	}
}

server {
	listen 80;
	server_name site.example.com;
	return 301 https://$host$request_uri;
}
`)
	rep := Read("", filepath.Join(root, "etc/nginx/vps-admin-apps"))
	if len(rep.Sites) != 1 {
		t.Fatalf("got %d sites, want only the HTTPS one: %+v", len(rep.Sites), rep.Sites)
	}
	if s := rep.Sites[0]; s.Scheme != "https" || s.Target != "127.0.0.1:3000" {
		t.Fatalf("site = %+v", s)
	}
}

func TestNoProxy(t *testing.T) {
	root := t.TempDir()
	rep := Read(filepath.Join(root, "etc/caddy/vps-admin-apps"), filepath.Join(root, "etc/nginx/vps-admin-apps"))
	if rep.Engine != "" || len(rep.Sites) != 0 {
		t.Fatalf("report = %+v, want nothing", rep)
	}
}

// The API sends these lists as JSON: nil would come out as null, which blanks a client
// that walks them.
func TestEmptyListsAreNotNil(t *testing.T) {
	root := t.TempDir()
	rep := Read(filepath.Join(root, "etc/caddy/vps-admin-apps"), filepath.Join(root, "etc/nginx/vps-admin-apps"))
	if rep.Sites == nil || rep.Unreadable == nil {
		t.Fatalf("report = %+v, want empty slices", rep)
	}
	write(t, root, "etc/caddy/Caddyfile", "import vps-admin-apps/*.caddy\n")
	write(t, root, "etc/caddy/vps-admin-apps/empty.caddy", "empty.example.com {\n\tfile_server\n}\n")
	rep = Read(filepath.Join(root, "etc/caddy/vps-admin-apps"), "")
	if len(rep.Sites) != 1 || rep.Sites[0].Domains == nil || rep.Unreadable == nil {
		t.Fatalf("report = %+v, want every list set", rep)
	}
}
