package system

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	OK      Status = "ok"
	Warn    Status = "warn"
	Fail    Status = "fail"
	Unknown Status = "unknown"
)

type Check struct {
	ID     string `json:"id"`
	Group  string `json:"group"` // ssh | network | updates
	Title  string `json:"title"`
	Status Status `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"` // what to do, shown when not ok
}

type Port struct {
	Proto   string `json:"proto"`
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
	Process string `json:"process,omitempty"`
	Public  bool   `json:"public"` // bound to all interfaces or a public address
}

type Security struct {
	Checks  []Check   `json:"checks"`
	Ports   []Port    `json:"ports"`
	Score   int       `json:"score"` // 0..100
	Checked time.Time `json:"checked"`
}

// Audit runs every check concurrently. Each one degrades to "unknown" instead of failing
// the whole report, since distributions differ in what is installed.
func (r *Runner) Audit(ctx context.Context) Security {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		checks []Check
		ports  []Port
	)
	add := func(c ...Check) {
		mu.Lock()
		checks = append(checks, c...)
		mu.Unlock()
	}
	run := func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	run(func() { add(r.sshChecks(ctx)...) })
	run(func() { add(r.firewallCheck(ctx)) })
	run(func() { add(r.fail2banCheck(ctx)) })
	run(func() { add(r.updateChecks(ctx)...) })
	run(func() { add(r.llmnrCheck(ctx)) })
	run(func() {
		p := r.listening(ctx)
		mu.Lock()
		ports = p
		mu.Unlock()
	})
	wg.Wait()

	order := []string{"ssh", "network", "updates"}
	slices.SortStableFunc(checks, func(a, b Check) int {
		if d := slices.Index(order, a.Group) - slices.Index(order, b.Group); d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})
	return Security{Checks: checks, Ports: ports, Score: score(checks), Checked: time.Now()}
}

func score(cs []Check) int {
	total, got := 0, 0
	for _, c := range cs {
		if c.Status == Unknown {
			continue
		}
		total += 2
		switch c.Status {
		case OK:
			got += 2
		case Warn:
			got++
		}
	}
	if total == 0 {
		return 0
	}
	return 100 * got / total
}

func (r *Runner) sshChecks(ctx context.Context) []Check {
	out, err := r.Sudo(ctx, 10*time.Second, "sshd", "-T")
	if err != nil {
		return []Check{{ID: "ssh", Group: "ssh", Title: "SSH configuration", Status: Unknown, Detail: "Could not read the effective sshd configuration: " + err.Error()}}
	}
	cfg := map[string][]string{}
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), " "); ok {
			cfg[k] = append(cfg[k], v)
		}
	}
	get := func(k string) string {
		if v := cfg[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	var cs []Check

	c := Check{ID: "ssh_port", Group: "ssh", Title: "SSH port", Status: OK, Detail: "Listening on " + strings.Join(cfg["port"], ", ")}
	if slices.Contains(cfg["port"], "22") {
		c.Status, c.Fix = Warn, "Move SSH to a high port to cut most automated scans. Open the new port in the firewall and test it before closing 22."
	}
	cs = append(cs, c)

	c = Check{ID: "ssh_password", Group: "ssh", Title: "Password login", Status: OK, Detail: "Only keys are accepted"}
	if get("passwordauthentication") == "yes" || get("kbdinteractiveauthentication") == "yes" {
		c.Status, c.Detail, c.Fix = Fail, "Passwords are accepted, so the server can be brute forced", "Set PasswordAuthentication no and KbdInteractiveAuthentication no once your key works."
	}
	cs = append(cs, c)

	c = Check{ID: "ssh_root", Group: "ssh", Title: "Root login", Status: OK, Detail: "Root cannot log in over SSH"}
	switch get("permitrootlogin") {
	case "yes":
		c.Status, c.Detail, c.Fix = Fail, "Root can log in, even with a password", "Set PermitRootLogin no and use sudo from a regular user."
	case "prohibit-password", "without-password", "forced-commands-only":
		c.Status, c.Detail, c.Fix = Warn, "Root can log in with a key", "Set PermitRootLogin no and use sudo from a regular user."
	}
	cs = append(cs, c)

	c = Check{ID: "ssh_x11", Group: "ssh", Title: "X11 forwarding", Status: OK, Detail: "Disabled"}
	if get("x11forwarding") == "yes" {
		c.Status, c.Detail, c.Fix = Warn, "Enabled, rarely needed on a server", "Set X11Forwarding no."
	}
	cs = append(cs, c)

	tries, _ := strconv.Atoi(get("maxauthtries"))
	c = Check{ID: "ssh_tries", Group: "ssh", Title: "Attempts per connection", Status: OK, Detail: fmt.Sprintf("%d attempts allowed", tries)}
	if tries > 4 {
		c.Status, c.Fix = Warn, "Set MaxAuthTries 3."
	}
	cs = append(cs, c)
	return cs
}

func (r *Runner) firewallCheck(ctx context.Context) Check {
	c := Check{ID: "firewall", Group: "network", Title: "Firewall"}
	if has("ufw") {
		if out, err := r.Sudo(ctx, 10*time.Second, "ufw", "status"); err == nil && strings.Contains(out, "Status: active") {
			c.Status, c.Detail = OK, "ufw is active"
			// A native nftables table with its own drop policy filters on top of ufw: a port
			// opened in ufw alone stays closed, which is easy to miss.
			if rules, err := r.Sudo(ctx, 10*time.Second, "nft", "list", "ruleset"); err == nil && nativeDropTable(rules) {
				c.Status = Warn
				c.Detail = "ufw is active, and a separate nftables table also drops input: open new ports in both"
				c.Fix = "Keep a single firewall, or remember to add every rule to /etc/nftables.conf as well."
			}
			return c
		}
	}
	if has("firewall-cmd") {
		if out, err := r.Run(ctx, 10*time.Second, "firewall-cmd", "--state"); err == nil && strings.TrimSpace(out) == "running" {
			c.Status, c.Detail = OK, "firewalld is running"
			return c
		}
	}
	if has("nft") {
		out, err := r.Sudo(ctx, 10*time.Second, "nft", "list", "ruleset")
		if err == nil && strings.Contains(out, "hook input") && strings.Contains(out, "policy drop") {
			c.Status, c.Detail = OK, "nftables drops unmatched input"
			return c
		}
		if err != nil {
			c.Status, c.Detail = Unknown, "Could not read the nftables ruleset: "+err.Error()
			return c
		}
	}
	c.Status, c.Detail = Fail, "No active firewall found: every listening port is reachable"
	c.Fix = "Enable ufw (or firewalld) with a default deny policy, after allowing your SSH port."
	return c
}

func (r *Runner) fail2banCheck(ctx context.Context) Check {
	c := Check{ID: "fail2ban", Group: "network", Title: "Brute force protection"}
	switch {
	case activeUnit(ctx, r, "fail2ban"):
		c.Status, c.Detail = OK, "fail2ban is running"
		if out, err := r.Sudo(ctx, 10*time.Second, "fail2ban-client", "status"); err == nil {
			if _, jails, ok := strings.Cut(out, "Jail list:"); ok {
				c.Detail += ", jails: " + strings.TrimSpace(jails)
			}
		}
	case activeUnit(ctx, r, "crowdsec"):
		c.Status, c.Detail = OK, "CrowdSec is running"
	default:
		c.Status, c.Detail, c.Fix = Warn, "Neither fail2ban nor CrowdSec is running", "Install fail2ban and enable the sshd jail."
	}
	return c
}

func activeUnit(ctx context.Context, r *Runner, name string) bool {
	out, _ := r.Run(ctx, 5*time.Second, "systemctl", "is-active", name)
	return strings.TrimSpace(out) == "active"
}

func (r *Runner) updateChecks(ctx context.Context) []Check {
	var cs []Check
	up := Check{ID: "updates", Group: "updates", Title: "Pending updates"}
	switch {
	case has("apt"):
		out, err := r.Run(ctx, 30*time.Second, "apt", "list", "--upgradable")
		if err != nil {
			up.Status, up.Detail = Unknown, err.Error()
			break
		}
		var names []string
		security := 0
		for _, l := range strings.Split(out, "\n") {
			if name, rest, ok := strings.Cut(l, "/"); ok && strings.Contains(rest, "upgradable") {
				names = append(names, name)
				if strings.Contains(rest, "-security") {
					security++
				}
			}
		}
		up.Status, up.Detail = updatesStatus(names, security)
	case has("dnf"):
		// dnf check-update exits 100 when updates exist.
		out, _ := r.Run(ctx, 60*time.Second, "dnf", "-q", "check-update")
		var names []string
		for _, l := range strings.Split(out, "\n") {
			if f := strings.Fields(l); len(f) == 3 && strings.Contains(f[0], ".") {
				names = append(names, f[0])
			}
		}
		up.Status, up.Detail = updatesStatus(names, 0)
	case has("checkupdates"): // Arch, from pacman-contrib; exits 2 when nothing is pending
		out, _ := r.Run(ctx, 60*time.Second, "checkupdates")
		var names []string
		for _, l := range strings.Split(out, "\n") {
			if f := strings.Fields(l); len(f) >= 4 && f[2] == "->" {
				names = append(names, f[0])
			}
		}
		up.Status, up.Detail = updatesStatus(names, 0)
	default:
		up.Status, up.Detail = Unknown, "Unsupported package manager"
	}
	if up.Status != OK {
		up.Fix = "Install them, then reboot if the kernel changed."
	}
	cs = append(cs, up)

	rb := Check{ID: "reboot", Group: "updates", Title: "Reboot", Status: OK, Detail: "Not needed"}
	if exists("/var/run/reboot-required") {
		rb.Status, rb.Detail, rb.Fix = Warn, "Updates are waiting for a reboot (usually a new kernel)", "Reboot when a short downtime is acceptable."
	}
	cs = append(cs, rb)

	au := Check{ID: "auto_updates", Group: "updates", Title: "Automatic security updates"}
	switch {
	case exists("/etc/apt/apt.conf.d/20auto-upgrades") && exists("/usr/bin/unattended-upgrade"):
		au.Status, au.Detail = OK, "unattended-upgrades is set up"
	case activeUnit(ctx, r, "dnf-automatic.timer") || activeUnit(ctx, r, "dnf-automatic-install.timer"):
		au.Status, au.Detail = OK, "dnf-automatic is enabled"
	default:
		au.Status, au.Detail, au.Fix = Warn, "Security fixes are only installed by hand", "Install unattended-upgrades (Debian, Ubuntu) or dnf-automatic (Fedora, RHEL)."
	}
	cs = append(cs, au)
	return cs
}

func updatesStatus(names []string, security int) (Status, string) {
	switch {
	case len(names) == 0:
		return OK, "The system is up to date"
	case security > 0:
		return Fail, fmt.Sprintf("%d updates, %d of them security fixes: %s", len(names), security, preview(names))
	default:
		return Warn, fmt.Sprintf("%d updates: %s", len(names), preview(names))
	}
}

func preview(names []string) string {
	if len(names) > 8 {
		return strings.Join(names[:8], ", ") + fmt.Sprintf(" and %d more", len(names)-8)
	}
	return strings.Join(names, ", ")
}

func (r *Runner) llmnrCheck(ctx context.Context) Check {
	c := Check{ID: "llmnr", Group: "network", Title: "LLMNR and mDNS"}
	if !has("resolvectl") {
		c.Status, c.Detail = OK, "systemd-resolved is not in use"
		return c
	}
	out, err := r.Run(ctx, 5*time.Second, "resolvectl", "status")
	switch {
	case err != nil:
		c.Status, c.Detail = OK, "systemd-resolved is not running"
	case strings.Contains(out, "+LLMNR") || strings.Contains(out, "+mDNS"):
		c.Status, c.Detail = Warn, "Local name resolution answers on the public network (port 5355)"
		c.Fix = "Set LLMNR=no and MulticastDNS=no in /etc/systemd/resolved.conf.d/."
	default:
		c.Status, c.Detail = OK, "Disabled"
	}
	return c
}

// listening lists TCP and UDP sockets. Process names need root, so it falls back to
// plain ss when sudo is not allowed.
func (r *Runner) listening(ctx context.Context) []Port {
	out, err := r.Sudo(ctx, 10*time.Second, "ss", "-tulnpH")
	if err != nil {
		if out, err = r.Run(ctx, 10*time.Second, "ss", "-tulnH"); err != nil {
			return nil
		}
	}
	return parseSS(out)
}

func parseSS(out string) []Port {
	seen := map[string]bool{}
	var ports []Port
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		local := f[4]
		i := strings.LastIndexByte(local, ':')
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(local[i+1:])
		if err != nil {
			continue
		}
		addr := strings.Trim(local[:i], "[]")
		if j := strings.IndexByte(addr, '%'); j >= 0 { // 0.0.0.0%lo
			addr = addr[:j]
		}
		p := Port{Proto: f[0], Addr: addr, Port: port}
		if len(f) > 6 {
			if _, rest, ok := strings.Cut(f[6], `(("`); ok {
				p.Process, _, _ = strings.Cut(rest, `"`)
			}
		}
		if a, err := netip.ParseAddr(addr); err == nil {
			p.Public = !(a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast())
		}
		key := fmt.Sprintf("%s/%d/%t", p.Proto, p.Port, p.Public)
		if seen[key] {
			continue
		}
		seen[key] = true
		ports = append(ports, p)
	}
	slices.SortFunc(ports, func(a, b Port) int { return a.Port - b.Port })
	return ports
}

// nativeDropTable reports a non-ufw table hooked on input with a drop policy.
func nativeDropTable(ruleset string) bool {
	table := ""
	for _, l := range strings.Split(ruleset, "\n") {
		l = strings.TrimSpace(l)
		if t, ok := strings.CutPrefix(l, "table "); ok {
			table = strings.TrimSuffix(t, " {")
			continue
		}
		// iptables-nft (ufw, docker) tables are "ip filter" / "ip6 filter"; fail2ban adds f2b-table.
		if table == "ip filter" || table == "ip6 filter" || strings.Contains(table, "f2b") {
			continue
		}
		if strings.Contains(l, "hook input") && strings.Contains(l, "policy drop") {
			return true
		}
	}
	return false
}
