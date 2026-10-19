// Package scripts runs the administrator's own scripts from the panel. Scripts are
// symlinked by root with `sudo vps-admin link FILE` into a root-owned folder: the agent
// can run them but cannot add or change them. They run as the vps-admin user, which
// has Docker access but no root.
package scripts

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultDir = "/etc/vps-admin/scripts"
	maxOutput  = 1 << 20
	maxRun     = 30 * time.Minute
)

var (
	nameRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	ErrNotFound = errors.New("script not found")
	ErrRunning  = errors.New("this script is already running")
)

type Script struct {
	Name        string `json:"name"`
	Target      string `json:"target"`      // the file the link points to
	Description string `json:"description"` // first comment line after the shebang
	Executable  bool   `json:"executable"`
	Missing     bool   `json:"missing"` // the target was moved or deleted
	Running     bool   `json:"running"`
}

// Link creates (or replaces) name -> file in dir. Run as root by the CLI.
func Link(dir, file, name string) (string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", errors.New("link a file, not a folder")
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	}
	if !nameRe.MatchString(name) {
		return "", fmt.Errorf("invalid name %q: letters, digits, dot, dash and underscore only", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	link := filepath.Join(dir, name)
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("%s exists and is not a link", link)
	}
	os.Remove(link)
	if err := os.Symlink(abs, link); err != nil {
		return "", err
	}
	if st.Mode()&0o111 == 0 {
		return name, fmt.Errorf("linked, but %s is not executable: run chmod +x %s", abs, abs)
	}
	return name, nil
}

// Unlink removes a script link (never the target file).
func Unlink(dir, name string) error {
	if !nameRe.MatchString(name) {
		return ErrNotFound
	}
	link := filepath.Join(dir, name)
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return ErrNotFound
	}
	return os.Remove(link)
}

func describe(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 8192))
	for i := 0; sc.Scan() && i < 10; i++ {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "#!") || l == "" || l == "#" {
			continue
		}
		if c, ok := strings.CutPrefix(l, "#"); ok {
			c = strings.TrimSpace(c)
			c = strings.TrimPrefix(c, "description:")
			return strings.TrimSpace(c)
		}
		break
	}
	return ""
}

type Runner struct {
	Dir     string
	DataDir string

	mu      sync.Mutex
	running map[string]*Run
}

// Run is one execution, kept in memory while it runs.
type Run struct {
	ID       string     `json:"id"`
	Script   string     `json:"script"`
	Args     []string   `json:"args"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	ExitCode *int       `json:"exitCode,omitempty"`
	Error    string     `json:"error,omitempty"`

	mu     sync.Mutex
	output strings.Builder
	subs   map[chan string]struct{}
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *Runner) Init() { r.running = map[string]*Run{} }

func (r *Runner) List() ([]Script, error) {
	entries, err := os.ReadDir(r.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Script{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Script{}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 || !nameRe.MatchString(e.Name()) {
			continue
		}
		link := filepath.Join(r.Dir, e.Name())
		target, _ := os.Readlink(link)
		s := Script{Name: e.Name(), Target: target}
		if st, err := os.Stat(link); err != nil {
			s.Missing = true
		} else {
			s.Executable = st.Mode()&0o111 != 0
			s.Description = describe(link)
		}
		r.mu.Lock()
		_, s.Running = r.running[s.Name]
		r.mu.Unlock()
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Start runs a script in the background. args are passed as-is, without a shell.
// onStart runs before the process starts (to record the run), onDone after it ends.
func (r *Runner) Start(name string, args []string, onStart, onDone func(*Run)) (*Run, error) {
	if !nameRe.MatchString(name) {
		return nil, ErrNotFound
	}
	link := filepath.Join(r.Dir, name)
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return nil, ErrNotFound
	}
	r.mu.Lock()
	if _, busy := r.running[name]; busy {
		r.mu.Unlock()
		return nil, ErrRunning
	}
	ctx, cancel := context.WithTimeout(context.Background(), maxRun)
	run := &Run{ID: fmt.Sprintf("%s-%d", name, time.Now().UnixMilli()), Script: name, Args: args, Started: time.Now(),
		subs: map[chan string]struct{}{}, cancel: cancel, done: make(chan struct{})}
	r.running[name] = run
	r.mu.Unlock()

	cmd := exec.CommandContext(ctx, link, args...)
	cmd.Dir = filepath.Dir(link)
	if target, err := filepath.EvalSymlinks(link); err == nil {
		cmd.Dir = filepath.Dir(target) // scripts often expect to run next to their files
	}
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + r.DataDir, "LANG=C.UTF-8", "TERM=dumb", "VPS_ADMIN=1"}
	cmd.Stdout, cmd.Stderr = run, run
	cmd.WaitDelay = 5 * time.Second
	if onStart != nil {
		onStart(run)
	}
	go func() {
		err := cmd.Run()
		now := time.Now()
		run.mu.Lock()
		run.Finished = &now
		code := cmd.ProcessState.ExitCode()
		if cmd.ProcessState != nil {
			run.ExitCode = &code
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			run.Error = "stopped after 30 minutes"
		} else if errors.Is(ctx.Err(), context.Canceled) {
			run.Error = "stopped from the panel"
		} else if err != nil && cmd.ProcessState == nil {
			run.Error = err.Error()
		}
		run.mu.Unlock()
		cancel()
		r.mu.Lock()
		delete(r.running, name)
		r.mu.Unlock()
		close(run.done)
		if onDone != nil {
			onDone(run)
		}
	}()
	return run, nil
}

func (run *Run) Write(p []byte) (int, error) {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.output.Len() < maxOutput {
		run.output.Write(p)
	}
	for ch := range run.subs {
		select {
		case ch <- string(p):
		default:
		}
	}
	return len(p), nil
}

// Output returns what the script printed so far.
func (run *Run) Output() string {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.output.String()
}

// Follow returns the output so far and a channel of what comes next; the channel
// stays open until cancel. Done tells when the run ended.
func (run *Run) Follow() (string, <-chan string, func()) {
	ch := make(chan string, 256)
	run.mu.Lock()
	out := run.output.String()
	run.subs[ch] = struct{}{}
	run.mu.Unlock()
	return out, ch, func() {
		run.mu.Lock()
		delete(run.subs, ch)
		run.mu.Unlock()
	}
}

func (run *Run) Done() <-chan struct{} { return run.done }

// Current returns the run in progress for a script, if any.
func (r *Runner) Current(name string) *Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[name]
}

func (r *Runner) Stop(name string) bool {
	if run := r.Current(name); run != nil {
		run.cancel()
		return true
	}
	return false
}

// SplitArgs splits an argument line like a shell would for simple cases: spaces
// separate, single or double quotes group. No expansion of any kind.
func SplitArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, c := range s {
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(c)
		case c == '\'' || c == '"':
			quote, inArg = c, true
		case c == ' ' || c == '\t':
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(c)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unclosed quote")
	}
	if inArg {
		out = append(out, cur.String())
	}
	return out, nil
}
