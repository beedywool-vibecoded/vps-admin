package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/beedywool/vps-admin/internal/buildinfo"
)

// installCmd reruns the installer from the latest release; data is untouched. Options
// pass through: sudo vps-admin install --domain panel.example.com
func installCmd(args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("run it with sudo: sudo vps-admin install")
	}
	repo := buildinfo.Repo
	if repo == "" {
		return errors.New("this build does not name a repository to install from")
	}
	url := "https://github.com/" + repo + "/releases/latest/download/install.sh"
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return fmt.Errorf("cannot download the installer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("cannot download the installer from %s: HTTP %d", repo, res.StatusCode)
	}
	// Read the whole script first so the client timeout can't cut stdin mid-install.
	script, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("cannot read the installer: %w", err)
	}

	cmd := exec.CommandContext(ctx, "bash", append([]string{"-s", "--"}, args...)...)
	cmd.Stdin = bytes.NewReader(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installer: %w", err)
	}
	return nil
}
