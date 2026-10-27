// Package assistant lets Claude administer the server through a chat, with the user's
// approval for every change.
package assistant

import (
	"regexp"
	"slices"
	"strings"
)

// Kind is how a tool call is handled.
type Kind string

const (
	Read    Kind = "read"    // runs without asking when the user allows it
	Change  Kind = "change"  // waits for the user's approval
	Blocked Kind = "blocked" // never runs, even approved
)

// neverAllowed is refused even when approved: destroying volumes, changing how the server
// is reached (SSH, firewall) or reading secrets. Those are done over SSH, knowingly.
var neverAllowed = []struct {
	re     *regexp.Regexp
	reason string
}{
	{regexp.MustCompile(`docker\s+(volume\s+(rm|prune)|system\s+prune.*--volumes)|compose\b.*\bdown\b.*(\s-v\b|--volumes)`), "deleting Docker volumes"},
	{regexp.MustCompile(`\b(mkfs|wipefs|fdisk|parted|sfdisk)\b|\bdd\b.*\bof=/dev/|\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r)[a-zA-Z]*\s+(/|/\*|~|/(etc|usr|var|home|root|boot|opt|srv))(\s|/?$)`), "destroying disks or system folders"},
	{regexp.MustCompile(`sshd_config|/etc/ssh/|\bauthorized_keys\b|\b(ufw|firewall-cmd)\s+(?:--\S+\s+)*(allow|deny|reject|limit|delete|disable|reset|insert|route|default|--add|--remove|--set|--reload|--permanent)|\bnft\s+(?:-\S+\s+)*(add|delete|flush|insert|replace|destroy|-f)\b|\b(ip6?tables)(-restore)?\b|/etc/nftables\.conf|systemctl\s+(stop|disable|mask|kill)\s+(ssh|sshd|ufw|nftables|firewalld|fail2ban|vps-admin)\b|\bpasswd\b|\busermod\b|\bchpasswd\b`), "changing SSH access, the firewall or accounts"},
	{regexp.MustCompile(`/etc/shadow|/etc/gshadow|id_(rsa|ed25519|ecdsa|dsa)\b|ssh_host_\w+_key\b|\.pem\b|\.key\b|secret\.key|vps-admin\.db|/etc/vps-admin/|\.env\.vps-admin|/root/`), "reading keys or private files"},
	{regexp.MustCompile(`\b(shutdown|poweroff|halt|init\s+0|systemctl\s+(poweroff|halt))\b`), "powering the server off"},
}

// readOnly are programs that only inspect. Pipes between them are fine; anything with
// redirections, command substitution or chaining needs approval.
var readOnly = []string{
	"cat", "head", "tail", "less", "grep", "egrep", "zgrep", "wc", "sort", "uniq", "cut", "tr", "column",
	"ls", "stat", "file", "du", "df", "free", "uptime", "uname", "hostname", "hostnamectl", "whoami", "id",
	"date", "timedatectl", "ps", "pgrep", "top", "ss", "netstat", "ip", "ping", "dig", "host", "nslookup",
	"getent", "lsblk", "findmnt", "mount", "lscpu", "lsof", "vmstat", "iostat", "nproc", "env", "printenv",
	"echo", "which", "journalctl", "dmesg", "last", "lastlog", "w", "who", "resolvectl", "timeout",
	"apt-cache", "dpkg-query", "openssl", "caddy", "wg", "fail2ban-client", "ufw", "nft", "sshd",
}

// readOnlySub are programs that are read-only for some subcommands only.
var readOnlySub = map[string][]string{
	"systemctl": {"status", "is-active", "is-enabled", "is-failed", "list-units", "list-unit-files", "list-timers", "show", "cat", "--failed"},
	"docker":    {"ps", "logs", "inspect", "images", "stats", "version", "info", "top", "port", "network", "volume", "compose"},
	"git":       {"status", "log", "diff", "show", "branch", "remote"},
	"apt":       {"list", "show", "policy", "search"},
	"dpkg":      {"-l", "-L", "-s", "--list", "--status"},
	"find":      {}, // checked below: no -exec / -delete
	"curl":      {}, // checked below: GET/HEAD only, no output file
}

// subcommands of docker that stay read-only only with these second words
var dockerSecond = map[string][]string{
	"network": {"ls", "inspect"},
	"volume":  {"ls", "inspect"},
	"compose": {"ps", "logs", "config", "ls", "images", "top"},
}

var (
	unsafeShell = regexp.MustCompile("[;&<>`]|\\$\\(|\\|\\||\\n")
	sensitiveCh = map[string]string{
		// subcommands of otherwise read-only tools that change things
		"ufw":             "status",
		"nft":             "list",
		"caddy":           "validate|version|list-modules|fmt|adapt|environ",
		"fail2ban-client": "status|ping|version|get|banned",
		"wg":              "show",
		"sshd":            "-T",
		"openssl":         "s_client|x509|version|ciphers",
		"ip":              "a|addr|address|r|route|link|neigh|-[46s]|-br|-c|-j",
		"timeout":         `\d+[smh]?`,
	}
)

// Classify decides how a shell command is handled. It runs as the agent user via `sh -c`.
func Classify(command string) (Kind, string) {
	c := strings.TrimSpace(command)
	if c == "" {
		return Blocked, "empty command"
	}
	for _, n := range neverAllowed {
		if n.re.MatchString(c) {
			return Blocked, "never allowed: " + n.reason
		}
	}
	if unsafeShell.MatchString(c) {
		return Change, "uses redirection or command chaining"
	}
	for _, seg := range strings.Split(c, "|") {
		words := strings.Fields(seg)
		if len(words) == 0 {
			return Change, "empty pipe segment"
		}
		if words[0] == "sudo" {
			return Change, "runs as root"
		}
		if !readOnlyWords(words) {
			return Change, words[0] + " can change the system"
		}
	}
	return Read, "read-only"
}

func readOnlyWords(w []string) bool {
	prog := w[0]
	if prog == "timeout" && len(w) > 2 { // timeout 10 <cmd>
		return regexp.MustCompile(`^\d+[smh]?$`).MatchString(w[1]) && readOnlyWords(w[2:])
	}
	if pat, ok := sensitiveCh[prog]; ok {
		if len(w) < 2 || !regexp.MustCompile(`^(`+pat+`)$`).MatchString(w[1]) {
			return prog == "ip" && len(w) == 1
		}
		if prog == "ip" {
			for _, a := range w[2:] {
				if slices.Contains([]string{"add", "del", "delete", "set", "flush", "change", "replace", "append", "up", "down"}, a) {
					return false
				}
			}
		}
		return true
	}
	if slices.Contains(readOnly, prog) {
		return true
	}
	subs, ok := readOnlySub[prog]
	if !ok {
		return false
	}
	switch prog {
	case "find":
		for _, a := range w[1:] {
			if a == "-exec" || a == "-execdir" || a == "-delete" || a == "-ok" || a == "-fprint" || a == "-fprintf" {
				return false
			}
		}
		return true
	case "curl":
		for _, a := range w[1:] {
			if strings.HasPrefix(a, "-o") || strings.HasPrefix(a, "--output") || a == "-O" || strings.HasPrefix(a, "-X") ||
				strings.HasPrefix(a, "--request") || strings.HasPrefix(a, "-d") || strings.HasPrefix(a, "--data") ||
				strings.HasPrefix(a, "-F") || strings.HasPrefix(a, "--form") || strings.HasPrefix(a, "-T") || strings.HasPrefix(a, "--upload") {
				return false
			}
		}
		return true
	}
	if len(w) < 2 || !slices.Contains(subs, w[1]) {
		return false
	}
	if prog == "docker" {
		if second, ok := dockerSecond[w[1]]; ok {
			return len(w) > 2 && slices.Contains(second, w[2])
		}
	}
	return true
}
