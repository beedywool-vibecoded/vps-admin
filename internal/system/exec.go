package system

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const maxOutput = 4 << 20

// Runner executes host commands. The agent runs as an unprivileged user; commands that
// need root go through `sudo -n`, which only allows what deploy/sudoers lists. When the
// agent itself runs as root (development, containers) sudo is skipped.
type Runner struct {
	root bool
}

func NewRunner() *Runner { return &Runner{root: os.Geteuid() == 0} }

type limitedBuffer struct {
	bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := maxOutput - b.Len(); room < len(p) {
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		b.truncated = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

// Run executes argv with a timeout and returns combined output.
func (r *Runner) Run(ctx context.Context, timeout time.Duration, argv ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	var out limitedBuffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	s := out.String()
	if out.truncated {
		s += "\n[output truncated]"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return s, fmt.Errorf("%s: timed out after %s", argv[0], timeout)
	}
	if err != nil {
		return s, fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, strings.TrimSpace(lastLine(s)))
	}
	return s, nil
}

// Sudo runs argv as root.
func (r *Runner) Sudo(ctx context.Context, timeout time.Duration, argv ...string) (string, error) {
	if r.root {
		return r.Run(ctx, timeout, argv...)
	}
	return r.Run(ctx, timeout, append([]string{"sudo", "-n", "--"}, argv...)...)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
