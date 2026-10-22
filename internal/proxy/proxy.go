// Package proxy reads the reverse proxy's configuration, so the panel can list every
// domain this machine publishes: the ones it writes itself and the ones added by hand.
package proxy

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Site is one published domain, or group of domains sharing a block, and where it sends
// traffic.
type Site struct {
	Domains []string `json:"domains"`
	// Target is the upstream the site forwards to, like 127.0.0.1:8080. Empty for a site
	// that only serves files or redirects.
	Target string `json:"target"`
	File   string `json:"file"`
	// Managed is set for the snippets VPS Admin writes, rewritten on every deploy.
	Managed bool   `json:"managed"`
	ID      string `json:"id"` // app id of a managed site, empty when the app is gone
	VPNOnly bool   `json:"vpnOnly"`
	Scheme  string `json:"scheme"` // https or http
}

// Report is the whole picture: which proxy answers, every site it serves, and the files
// that exist but could not be read (the agent runs as vps-admin, not root).
type Report struct {
	Engine     string   `json:"engine"` // caddy, nginx, or "" when neither is installed
	Root       string   `json:"root"`   // configuration directory of the proxy
	Sites      []Site   `json:"sites"`
	Unreadable []string `json:"unreadable"`
}

// Read reports what the machine's reverse proxy serves. caddySites and nginxSites are the
// directories the panel writes its own snippets to; the proxy is the one that has them,
// then the one installed on the machine.
func Read(caddySites, nginxSites string) Report {
	caddyRoot, nginxRoot := parent(caddySites), parent(nginxSites)
	switch {
	case isDir(caddySites) || isFile(filepath.Join(caddyRoot, "Caddyfile")):
		return normalize(readCaddy(caddyRoot, caddySites))
	case isDir(nginxSites) || isDir(nginxRoot):
		return normalize(readNginx(nginxRoot, nginxSites))
	}
	return normalize(Report{})
}

// normalize gives every list a value, so the JSON says "sites": [] instead of null: a
// client that walks it has nothing to guard against.
func normalize(r Report) Report {
	if r.Sites == nil {
		r.Sites = []Site{}
	}
	if r.Unreadable == nil {
		r.Unreadable = []string{}
	}
	for i := range r.Sites {
		if r.Sites[i].Domains == nil {
			r.Sites[i].Domains = []string{}
		}
	}
	return r
}

// ---------------------------------------------------------------------------------
// Caddy

func readCaddy(root, apps string) Report {
	rep := Report{Engine: "caddy", Root: root}
	seen := map[string]bool{}
	readCaddyFile(filepath.Join(root, "Caddyfile"), root, apps, 0, seen, &rep)
	finish(&rep)
	return rep
}

// readCaddyFile reads a Caddyfile and the files its import lines point at, so the sites of
// the panel's snippets are listed exactly when Caddy loads them.
func readCaddyFile(path, root, apps string, depth int, seen map[string]bool, rep *Report) {
	if depth > 4 || seen[path] {
		return
	}
	seen[path] = true
	src, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			rep.Unreadable = append(rep.Unreadable, path)
		}
		return
	}
	sites, imports := parseCaddy(string(src), path, apps)
	rep.Sites = append(rep.Sites, sites...)
	for _, pattern := range imports {
		files, _ := filepath.Glob(filepath.Join(root, pattern))
		for _, f := range files {
			readCaddyFile(f, root, apps, depth+1, seen, rep)
		}
	}
}

// parseCaddy returns the sites of a Caddyfile and the import patterns it asks for. A site
// block is an address list followed by braces; the global options block is the one with
// no address.
func parseCaddy(src, file, apps string) ([]Site, []string) {
	var sites []Site
	var imports []string
	var cur *Site
	vpn := false
	depth := 0

	closeBlock := func() {
		if cur != nil && len(cur.Domains) > 0 {
			cur.VPNOnly = vpn
			if cur.Managed {
				cur.ID = strings.TrimSuffix(filepath.Base(file), ".caddy")
			}
			sites = append(sites, *cur)
		}
		cur, vpn = nil, false
	}

	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(comment(raw))
		if line == "" {
			continue
		}
		if depth == 0 && strings.HasPrefix(line, "import ") {
			imports = append(imports, strings.TrimSpace(strings.TrimPrefix(line, "import")))
			continue
		}
		if strings.HasPrefix(line, "}") {
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				closeBlock()
			}
			continue
		}
		if strings.HasSuffix(line, "{") {
			if depth == 0 {
				cur = &Site{File: file, Managed: inside(file, apps), Scheme: "https"}
				cur.Domains, cur.Scheme = caddyAddresses(strings.TrimSuffix(line, "{"))
			}
			depth++
			continue
		}
		if cur == nil {
			continue
		}
		fields := strings.Fields(line)
		if fields[0] == "reverse_proxy" && cur.Target == "" && len(fields) > 1 {
			cur.Target = upstream(fields[1])
		}
		if strings.Contains(line, "not remote_ip") {
			vpn = true
		}
	}
	closeBlock()
	return sites, imports
}

// caddyAddresses splits a site block's header into domains and a scheme. Caddy answers
// HTTPS for a name unless the address says otherwise; a bare port is plain HTTP.
func caddyAddresses(head string) ([]string, string) {
	scheme := "https"
	var domains []string
	split := func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }
	for _, part := range strings.FieldsFunc(head, split) {
		addr := part
		if i := strings.Index(addr, "://"); i >= 0 {
			if addr[:i] == "http" {
				scheme = "http"
			}
			addr = addr[i+3:]
		}
		if strings.HasPrefix(addr, ":") {
			scheme = "http"
			continue
		}
		if name := hostOnly(addr); name != "" {
			domains = append(domains, name)
		}
	}
	return domains, scheme
}

// ---------------------------------------------------------------------------------
// nginx

// nginxServer is a server block while it is being read.
type nginxServer struct {
	site    Site
	ssl     bool
	allow   bool
	denyAll bool
	// redirect is set for the plain HTTP block that only sends visitors to the HTTPS one.
	redirect bool
}

func readNginx(root, apps string) Report {
	rep := Report{Engine: "nginx", Root: root}
	upstreams := map[string]string{}
	seen := map[string]bool{}
	var servers []*nginxServer

	add := func(path string) {
		st, err := os.Stat(path)
		if err != nil || st.IsDir() || seen[path] {
			return
		}
		seen[path] = true
		src, err := os.ReadFile(path)
		if err != nil {
			rep.Unreadable = append(rep.Unreadable, path)
			return
		}
		servers = append(servers, parseNginx(string(src), path, apps, upstreams)...)
	}
	add(filepath.Join(root, "nginx.conf"))
	addDir(filepath.Join(root, "sites-enabled"), add)
	addDir(filepath.Join(root, "conf.d"), add)
	addDir(apps, add)

	for _, s := range servers {
		if len(s.site.Domains) == 0 {
			continue
		}
		// proxy_pass names an upstream block more often than not.
		if target, ok := upstreams[s.site.Target]; ok && target != "" {
			s.site.Target = target
		}
		s.site.Scheme = "http"
		if s.ssl {
			s.site.Scheme = "https"
		}
		s.site.VPNOnly = s.allow && s.denyAll
		if s.site.Managed {
			s.site.ID = strings.TrimSuffix(filepath.Base(s.site.File), ".conf")
		}
		if s.redirect && s.site.Scheme == "http" {
			// A second block for the same name that only pushes visitors to HTTPS.
			if servedOverTLS(servers, s.site.Domains) {
				continue
			}
		}
		rep.Sites = append(rep.Sites, s.site)
	}
	finish(&rep)
	return rep
}

// parseNginx returns the server blocks of an nginx file. Upstream names are collected so
// proxy_pass http://name can be resolved once every file is read.
func parseNginx(src, file, apps string, upstreams map[string]string) []*nginxServer {
	var out []*nginxServer
	managed := inside(file, apps)
	var cur *nginxServer
	depth, serverDepth := 0, -1
	var up *string
	upDepth := -1

	closeServer := func() {
		if cur != nil {
			out = append(out, cur)
		}
		cur, serverDepth = nil, -1
	}

	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(comment(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "}") {
			if depth > 0 {
				depth--
			}
			if cur != nil && depth < serverDepth {
				closeServer()
			}
			if up != nil && depth < upDepth {
				up, upDepth = nil, -1
			}
			continue
		}
		if strings.HasSuffix(line, "{") {
			head := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			switch {
			case head == "server" || strings.HasPrefix(head, "server "):
				closeServer()
				cur = &nginxServer{site: Site{File: file, Managed: managed}}
				serverDepth = depth + 1
			case strings.HasPrefix(head, "upstream "):
				if f := strings.Fields(head); len(f) > 1 {
					upstreams[f[1]] = ""
					name := f[1]
					up, upDepth = &name, depth+1
				}
			}
			depth++
			continue
		}
		fields := strings.Fields(line)
		if fields[0] == "include" {
			continue
		}
		if up != nil && depth == upDepth && fields[0] == "server" && len(fields) > 1 {
			upstreams[*up] = upstream(fields[1])
			continue
		}
		if cur == nil {
			continue
		}
		switch fields[0] {
		case "server_name":
			cur.site.Domains = append(cur.site.Domains, nginxNames(line)...)
		case "listen":
			if strings.Contains(line, "443") || strings.Contains(line, "ssl") {
				cur.ssl = true
			}
		case "ssl_certificate":
			cur.ssl = true
		case "proxy_pass":
			if len(fields) > 1 && cur.site.Target == "" {
				cur.site.Target = upstream(fields[1])
			}
		case "allow":
			cur.allow = true
		case "deny":
			if len(fields) > 1 && strings.TrimSuffix(fields[1], ";") == "all" {
				cur.denyAll = true
			}
		case "return":
			if len(fields) > 1 && strings.HasPrefix(fields[1], "30") && strings.Contains(line, "https://") {
				cur.redirect = true
			}
		}
	}
	closeServer()
	return out
}

// nginxNames returns the hostnames of a server_name line, without the catch-alls that
// cannot be opened as a link.
func nginxNames(line string) []string {
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "server_name"))
	line = strings.TrimSuffix(line, ";")
	var out []string
	for _, name := range strings.Fields(line) {
		name = strings.Trim(name, `"'`)
		if name == "" || name == "_" {
			continue
		}
		out = append(out, name)
	}
	return out
}

// servedOverTLS tells whether another block serves one of these names over HTTPS.
func servedOverTLS(all []*nginxServer, domains []string) bool {
	for _, s := range all {
		if !s.ssl || len(s.site.Domains) == 0 {
			continue
		}
		if sameDomain(s.site.Domains, domains) {
			return true
		}
	}
	return false
}

func sameDomain(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if strings.EqualFold(x, y) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------------
// Shared

// finish drops repeated entries, then orders the sites by their first domain.
func finish(rep *Report) {
	seen := map[string]bool{}
	var kept []Site
	for _, s := range rep.Sites {
		key := s.Scheme + "|" + strings.Join(s.Domains, ",") + "|" + s.Target + "|" + s.File
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, s)
	}
	rep.Sites = kept
	sort.SliceStable(rep.Sites, func(i, j int) bool {
		a, b := strings.ToLower(rep.Sites[i].Domains[0]), strings.ToLower(rep.Sites[j].Domains[0])
		if a != b {
			return a < b
		}
		return rep.Sites[i].File < rep.Sites[j].File
	})
}

// upstream keeps the host and port of a directive's target: the scheme, any path, and a
// trailing semicolon are of no use in the list.
func upstream(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimSuffix(v, ";")
	for _, prefix := range []string{"http://", "https://"} {
		v = strings.TrimPrefix(v, prefix)
	}
	if strings.HasPrefix(v, "unix:") {
		return strings.TrimSuffix(strings.TrimSpace(v), ":/")
	}
	if i := strings.Index(v, "/"); i >= 0 {
		v = v[:i]
	}
	return v
}

// hostOnly drops a port and the brackets of an IPv6 address.
func hostOnly(addr string) string {
	if strings.HasPrefix(addr, "[") {
		if i := strings.Index(addr, "]"); i > 0 {
			return addr[1:i]
		}
	}
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// comment removes an end-of-line comment.
func comment(line string) string {
	if i := strings.Index(line, "#"); i >= 0 {
		return line[:i]
	}
	return line
}

// inside tells whether path is the directory dir or something under it.
func inside(path, dir string) bool {
	if dir == "" || path == "" {
		return false
	}
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

func parent(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Dir(strings.TrimSuffix(p, "/"))
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return p != "" && err == nil && st.IsDir()
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return p != "" && err == nil && !st.IsDir()
}

// addDir calls add on every file of a directory, following symlinks (sites-enabled).
func addDir(dir string, add func(string)) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		add(filepath.Join(dir, e.Name()))
	}
}
