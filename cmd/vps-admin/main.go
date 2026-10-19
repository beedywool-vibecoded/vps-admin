// Command vps-admin is the server agent: API, embedded panel, metrics and self-update.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/beedywool/vps-admin/internal/api"
	"github.com/beedywool/vps-admin/internal/assistant"
	"github.com/beedywool/vps-admin/internal/auth"
	"github.com/beedywool/vps-admin/internal/buildinfo"
	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/github"
	"github.com/beedywool/vps-admin/internal/scripts"
	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
	"github.com/beedywool/vps-admin/internal/system"
	"github.com/beedywool/vps-admin/internal/update"
)

const usage = `Usage: vps-admin <command> [-config path]

Commands:
  serve     run the agent (what the systemd unit starts)
  pair      print a one-time code to connect a browser or the app
  link      link a script so the panel can run it: sudo vps-admin link FILE [-name NAME]
  unlink    remove a script link: sudo vps-admin unlink NAME
  scripts   list linked scripts
  version   print the version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfgPath := fs.String("config", config.DefaultPath, "configuration file")
	linkName := fs.String("name", "", "name shown in the panel (link only, defaults to the file name)")
	linkDir := fs.String("dir", "", "compose folder the script belongs to (link only, found from the file location by default)")
	// Flags may come after the file: vps-admin link FILE -name NAME
	var pos []string
	rest := os.Args[2:]
	for len(rest) > 0 {
		fs.Parse(rest)
		rest = fs.Args()
		if len(rest) > 0 {
			pos = append(pos, rest[0])
			rest = rest[1:]
		}
	}

	var err error
	switch cmd {
	case "version", "--version", "-v":
		fmt.Println(buildinfo.Version)
		return
	case "serve":
		err = serve(*cfgPath)
	case "pair":
		err = pair(*cfgPath)
	case "link", "unlink", "scripts":
		err = scriptsCmd(cmd, *cfgPath, pos, *linkName, *linkDir)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "vps-admin:", err)
		os.Exit(1)
	}
}

func pair(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("%w (run it as root or as the vps-admin user)", err)
	}
	defer st.Close()
	a, err := auth.New(cfg, st)
	if err != nil {
		return err
	}
	code, err := a.NewPairingCode(context.Background())
	if err != nil {
		return err
	}
	// Created as root, the database must stay readable by the service user.
	chownData(cfg.DataDir)
	fmt.Printf("\n  Pairing code:  %s\n\n  Open %s in a browser, or add this server in the app.\n  The code works once and expires in %d minutes.\n\n",
		code, cfg.PublicURL, int(auth.PairingTTL/time.Minute))
	return nil
}

func serve(cfgPath string) error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	a, err := auth.New(cfg, st)
	if err != nil {
		return err
	}
	up, err := update.New(cfg.DataDir)
	if err != nil {
		return err
	}
	up.Token = cfg.Update.GitHubToken

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	box, err := secret.Open(cfg.DataDir)
	if err != nil {
		return err
	}

	collector := system.NewCollector()
	go collector.Run(ctx)

	runner := system.NewRunner()
	gh := github.New()
	// For tests against a fake GitHub; never set in production.
	if v := os.Getenv("VPS_ADMIN_GITHUB_API"); v != "" {
		gh.API = v
	}
	engine := &deploy.Engine{
		Store: st, Box: box, GitHub: gh, DataDir: cfg.DataDir, SitesDir: cfg.CaddySitesDir, NginxDir: cfg.NginxSitesDir,
		ReloadCaddy: func(ctx context.Context) error { return reloadCaddy(ctx, runner) },
		TailscaleName: func(ctx context.Context) string {
			if ts := runner.DetectEnvironment(ctx).Tailscale; ts != nil && ts.Running {
				return ts.Name
			}
			return ""
		},
	}
	// The root helper for nginx, certbot and tailscale serve (installed with the agent).
	if _, err := os.Stat("/opt/vps-admin/proxy-helper"); err == nil {
		engine.Helper = func(ctx context.Context, args ...string) (string, error) {
			return runner.Sudo(ctx, 5*time.Minute, append([]string{"/opt/vps-admin/proxy-helper"}, args...)...)
		}
	}
	engine.Init()
	go engine.Run(ctx)

	scriptRunner := &scripts.Runner{Dir: cfg.ScriptsDir, DataDir: cfg.DataDir}
	if _, err := os.Stat("/opt/vps-admin/run-script"); err == nil {
		scriptRunner.Helper = "/opt/vps-admin/run-script"
	}
	scriptRunner.Init()
	st.InterruptedScriptRuns(ctx)

	dockerClient := docker.New(cfg.DockerSocket)
	host := system.ReadHost()
	asst := &assistant.Engine{
		Deps: assistant.Deps{Store: st, Runner: runner, Docker: dockerClient, Collector: collector, Deploy: engine, Scripts: scriptRunner, Host: host, DataDir: cfg.DataDir},
		Box:  box,
		// For tests against a fake API; never set in production.
		BaseURL: os.Getenv("VPS_ADMIN_ANTHROPIC_URL"),
	}
	asst.Init(ctx)

	srv := &api.Server{
		Cfg: cfg, Store: st, Auth: a, Collector: collector, Runner: runner,
		Docker: dockerClient, Updater: up, Host: host,
		Box: box, GitHub: gh, Deploy: engine, Assistant: asst, Scripts: scriptRunner,
		Restart: stop,
	}
	srv.LoadGitHub(ctx)
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	slog.Info("vps-admin started", "version", buildinfo.Version, "listen", cfg.Listen, "public_url", cfg.PublicURL)

	go srv.AutoUpdateLoop(ctx)
	go housekeeping(ctx, st, up)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdown)
}

// housekeeping confirms a fresh update once the agent has run for a minute, then prunes
// the audit log daily.
func housekeeping(ctx context.Context, st *store.Store, up *update.Updater) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Minute):
		up.Commit()
	}
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		st.PruneAudit(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// chownData hands files created by `sudo vps-admin pair` back to the service user.
func chownData(dir string) {
	if os.Geteuid() != 0 {
		return
	}
	info, err := os.Stat(dir)
	if err != nil {
		return
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok || sys.Uid == 0 {
		return
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		os.Lchown(dir+"/"+e.Name(), int(sys.Uid), int(sys.Gid))
	}
}

// scriptsCmd manages the script links. Linking needs root: the folder is root-owned so
// the agent itself cannot add scripts.
func scriptsCmd(cmd, cfgPath string, args []string, name, folder string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	switch cmd {
	case "link":
		if len(args) != 1 {
			return errors.New("usage: sudo vps-admin link FILE [-name NAME]")
		}
		n, err := scripts.Link(cfg.ScriptsDir, args[0], name, folder)
		if n != "" {
			fmt.Printf("Linked %s as %q: run it from the Scripts page of the panel.\n", args[0], n)
			if d := (&scripts.Runner{Dir: cfg.ScriptsDir}).FolderOf(n); d != "" {
				fmt.Printf("It also shows with the containers of %s.\n", d)
			}
		}
		return err
	case "unlink":
		if len(args) != 1 {
			return errors.New("usage: sudo vps-admin unlink NAME")
		}
		if err := scripts.Unlink(cfg.ScriptsDir, args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed %q (the file itself is untouched).\n", args[0])
		return nil
	default:
		r := &scripts.Runner{Dir: cfg.ScriptsDir}
		r.Init()
		list, err := r.List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("No script linked yet. Add one with: sudo vps-admin link FILE")
		}
		for _, s := range list {
			state := ""
			if s.Missing {
				state = "  (target missing)"
			} else if !s.Executable {
				state = "  (not executable)"
			}
			fmt.Printf("%-24s -> %s%s\n", s.Name, s.Target, state)
		}
		return nil
	}
}

// reloadCaddy applies the app snippets. A reload can fail while Caddy is busy (another
// reload, certificates being obtained), so it is retried; the final error carries
// Caddy's own message from the journal rather than systemctl's exit status.
func reloadCaddy(ctx context.Context, runner *system.Runner) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
		if _, err = runner.Sudo(ctx, time.Minute, "systemctl", "reload", "caddy.service"); err == nil {
			return nil
		}
	}
	out, _ := exec.CommandContext(ctx, "journalctl", "-u", "caddy", "--since", "-2min", "-o", "cat", "--no-pager").Output()
	var last string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, `"level":"error"`) || strings.HasPrefix(line, "Error:") {
			last = line
		}
	}
	if last != "" {
		if i := strings.Index(last, `"error":"`); i >= 0 {
			last = strings.TrimSuffix(last[i+9:], `"}`)
		}
		return fmt.Errorf("%w: %s", err, last)
	}
	return err
}
