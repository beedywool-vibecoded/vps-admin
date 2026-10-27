// Package system reads host metrics, services and the security posture of the server.
package system

import (
	"bufio"
	"bytes"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Proc is the procfs root, changed by tests.
var Proc = "/proc"

type Host struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
	Arch     string `json:"arch"`
	CPUModel string `json:"cpuModel"`
	Cores    int    `json:"cores"`
}

type Disk struct {
	Mount string `json:"mount"`
	FS    string `json:"fs"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

type Snapshot struct {
	Time      time.Time  `json:"time"`
	Uptime    float64    `json:"uptime"` // seconds
	CPU       float64    `json:"cpu"`    // percent over the last sample, all cores
	Load      [3]float64 `json:"load"`
	MemTotal  uint64     `json:"memTotal"`
	MemUsed   uint64     `json:"memUsed"` // total minus available
	SwapTotal uint64     `json:"swapTotal"`
	SwapUsed  uint64     `json:"swapUsed"`
	NetRx     float64    `json:"netRx"` // bytes per second, physical interfaces
	NetTx     float64    `json:"netTx"`
	Disks     []Disk     `json:"disks"`
}

func readFile(name string) []byte {
	b, _ := os.ReadFile(Proc + "/" + name)
	return b
}

func ReadHost() Host {
	h := Host{Arch: runtime.GOARCH, Cores: runtime.NumCPU()}
	h.Hostname, _ = os.Hostname()
	h.Kernel = strings.TrimSpace(string(readFile("sys/kernel/osrelease")))
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
				h.OS = strings.Trim(v, `"`)
			}
		}
	}
	for _, l := range strings.Split(string(readFile("cpuinfo")), "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "model name" {
			h.CPUModel = strings.TrimSpace(v)
			break
		}
	}
	return h
}

type cpuTimes struct{ idle, total uint64 }

func readCPU() cpuTimes {
	line, _, _ := bytes.Cut(readFile("stat"), []byte("\n"))
	f := strings.Fields(string(line))
	var t cpuTimes
	if len(f) < 5 || f[0] != "cpu" {
		return t
	}
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		// guest and guest_nice are already counted in user and nice
		if i >= 8 {
			break
		}
		t.total += n
		if i == 3 || i == 4 { // idle, iowait
			t.idle += n
		}
	}
	return t
}

func cpuPercent(a, b cpuTimes) float64 {
	dt := float64(b.total - a.total)
	if b.total <= a.total || dt == 0 {
		return 0
	}
	return 100 * (1 - float64(b.idle-a.idle)/dt)
}

func readMem() (total, avail, swapTotal, swapFree uint64) {
	sc := bufio.NewScanner(bytes.NewReader(readFile("meminfo")))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		n, _ := strconv.ParseUint(f[1], 10, 64)
		n *= 1024
		switch f[0] {
		case "MemTotal:":
			total = n
		case "MemAvailable:":
			avail = n
		case "SwapTotal:":
			swapTotal = n
		case "SwapFree:":
			swapFree = n
		}
	}
	return
}

func readLoad() (l [3]float64) {
	f := strings.Fields(string(readFile("loadavg")))
	for i := 0; i < 3 && i < len(f); i++ {
		l[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return
}

func readUptime() float64 {
	f := strings.Fields(string(readFile("uptime")))
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

// virtualIface skips loopback, containers, bridges and tunnels.
func virtualIface(name string) bool {
	for _, p := range []string{"lo", "docker", "br-", "veth", "virbr", "wg", "tun", "tap", "tailscale", "cni", "flannel", "kube"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func readNet() (rx, tx uint64) {
	sc := bufio.NewScanner(bytes.NewReader(readFile("net/dev")))
	for sc.Scan() {
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		if virtualIface(strings.TrimSpace(name)) {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(f[0], 10, 64)
		t, _ := strconv.ParseUint(f[8], 10, 64)
		rx += r
		tx += t
	}
	return
}

var realFS = map[string]bool{"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true, "f2fs": true, "vfat": true, "jfs": true, "reiserfs": true, "bcachefs": true}

func readDisks() []Disk {
	out := []Disk{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(readFile("mounts")))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 3 || !realFS[f[2]] || seen[f[0]] {
			continue
		}
		// Mount points escape spaces as \040
		mount := strings.ReplaceAll(f[1], `\040`, " ")
		if strings.HasPrefix(mount, "/var/lib/docker") || strings.HasPrefix(mount, "/snap") || strings.HasPrefix(mount, "/boot/efi") {
			continue
		}
		var st syscall.Statfs_t
		if syscall.Statfs(mount, &st) != nil || st.Blocks == 0 {
			continue
		}
		seen[f[0]] = true
		bs := uint64(st.Bsize)
		out = append(out, Disk{Mount: mount, FS: f[2], Total: st.Blocks * bs, Used: (st.Blocks - st.Bfree) * bs})
	}
	return out
}
