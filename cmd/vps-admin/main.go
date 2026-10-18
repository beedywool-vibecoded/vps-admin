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
	"os/signal"
	"syscall"
	"time"

	"github.com/beedywool/vps-admin/internal/api"
	"github.com/beedywool/vps-admin/internal/auth"
	"github.com/beedywool/vps-admin/internal/buildinfo"
	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/store"
	"github.com/beedywool/vps-admin/internal/system"
	"github.com/beedywool/vps-admin/internal/update"
)

const usage = `Usage: vps-admin <command> [-config path]

Commands:
  serve     run the agent (what the systemd unit starts)
  pair      print a one-time code to connect a browser or the app
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
	fs.Parse(os.Args[2:])

	var err error
	switch cmd {
	case "version", "--version", "-v":
		fmt.Println(buildinfo.Version)
		return
	case "serve":
		err = serve(*cfgPath)
	case "pair":
		err = pair(*cfgPath)
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

	collector := system.NewCollector()
	go collector.Run(ctx)

	srv := &api.Server{
		Cfg: cfg, Store: st, Auth: a, Collector: collector, Runner: system.NewRunner(),
		Docker: docker.New(cfg.DockerSocket), Updater: up, Host: system.ReadHost(),
		Restart: stop,
	}
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
