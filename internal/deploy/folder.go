package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/beedywool/vps-admin/internal/store"
)

// Existing folders: a git checkout with a compose file, set up by hand before VPS Admin
// (or by anything else). Pushes update it in place: git fast-forwards to the pushed
// commit, then compose rebuilds. Both run in throwaway containers, so the agent needs no
// rights on the folder beyond Docker, and files keep their owner.

const (
	gitImage     = "alpine/git:2.45.2"
	composeImage = "docker:27-cli"
)

var ErrNotGit = errors.New("this folder is not a git checkout")

// owner returns "uid:gid" of dir, so git writes files as their owner.
func owner(dir string) (string, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("cannot read the folder owner")
	}
	return fmt.Sprintf("%d:%d", sys.Uid, sys.Gid), nil
}

// git runs a git command in dir, inside a container, as the folder owner.
func (e *Engine) git(ctx context.Context, out io.Writer, dir string, args ...string) error {
	own, err := owner(dir)
	if err != nil {
		return err
	}
	full := append([]string{"run", "--rm", "--network", "host", "--user", own, "-v", dir + ":" + dir, "-w", dir,
		"-e", "HOME=/tmp", "-e", "GIT_TERMINAL_PROMPT=0", gitImage, "-c", "safe.directory=*"}, args...)
	return e.docker(ctx, out, "", nil, full...)
}

func (e *Engine) gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	var b strings.Builder
	err := e.git(ctx, &b, dir, args...)
	return strings.TrimSpace(b.String()), err
}

// compose runs docker compose for a folder from a container that sees the folder at the
// same path, so relative bind mounts and env files resolve as they would on the host.
func (e *Engine) compose(ctx context.Context, out io.Writer, app store.App, dir string, args ...string) error {
	full := []string{"run", "--rm", "-v", "/var/run/docker.sock:/var/run/docker.sock", "-v", dir + ":" + dir, "-w", dir, composeImage,
		"docker", "compose", "-p", e.projectName(app)}
	if app.ComposeFile != "" {
		full = append(full, "-f", app.ComposeFile)
	}
	return e.docker(ctx, out, "", nil, append(full, args...)...)
}

// projectName keeps the existing compose project of a linked folder, so its containers
// are updated instead of duplicated.
func (e *Engine) projectName(app store.App) string {
	if app.Dir != "" {
		return app.ID
	}
	return e.composeProject(app.ID)
}

var ghRemote = regexp.MustCompile(`github\.com[:/]([^/\s]+/[^/\s]+?)(\.git)?/?$`)

// FolderInfo describes a folder before it is linked.
type FolderInfo struct {
	Repo   string `json:"repo"`   // owner/name when the origin is on GitHub
	Branch string `json:"branch"` // current branch
	Head   string `json:"head"`
}

func (e *Engine) InspectFolder(ctx context.Context, dir string) (*FolderInfo, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("absolute path expected")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return nil, ErrNotGit
	}
	info := &FolderInfo{}
	url, _ := e.gitOut(ctx, dir, "remote", "get-url", "origin")
	if m := ghRemote.FindStringSubmatch(url); m != nil {
		info.Repo = m[1]
	}
	info.Branch, _ = e.gitOut(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	info.Head, _ = e.gitOut(ctx, dir, "rev-parse", "HEAD")
	return info, nil
}

// CurrentCommit is what an app runs now: the folder HEAD, or the live deployment.
func (e *Engine) CurrentCommit(ctx context.Context, app store.App) string {
	if app.Dir != "" {
		head, _ := e.gitOut(ctx, app.Dir, "rev-parse", "HEAD")
		return head
	}
	if live, err := e.Store.LiveDeployment(ctx, app.ID); err == nil {
		return live.SHA
	}
	return ""
}

// updateFolder brings a linked folder to sha: fast-forward for normal pushes, a hard
// reset for rollbacks (asked for explicitly), then compose up.
func (e *Engine) updateFolder(ctx context.Context, l *logger, app store.App, d *store.Deployment) error {
	if d.SHA == "" {
		c, err := e.GitHub.Commit(ctx, app.InstallationID, app.Repo, app.Branch)
		if err != nil {
			return fmt.Errorf("read %s@%s: %w", app.Repo, app.Branch, err)
		}
		d.SHA, d.Message = c.SHA, c.Message
		e.Store.SaveDeployment(ctx, d)
	}
	tok, err := e.GitHub.Token(ctx, app.InstallationID)
	if err != nil {
		return err
	}
	// The token only lives in this command line, never in the folder's git config.
	remote := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", tok, app.Repo)
	if e.gitRemote != nil {
		remote = e.gitRemote(app.Repo, tok)
	}
	l.Printf("Fetching %s@%s into %s", app.Repo, app.Branch, app.Dir)
	if err := e.git(ctx, io.Discard, app.Dir, "fetch", "--quiet", remote, app.Branch); err != nil {
		return fmt.Errorf("git fetch failed (is the folder owner able to write it?): %w", err)
	}
	if d.Trigger == "rollback" {
		l.Printf("Rolling back to %s", short(d.SHA))
		if err := e.git(ctx, l, app.Dir, "reset", "--hard", d.SHA); err != nil {
			return err
		}
	} else if err := e.git(ctx, l, app.Dir, "merge", "--ff-only", d.SHA); err != nil {
		return errors.New("the folder has local changes or diverged from GitHub; commit or discard them on the server, then update again")
	}
	d.Build = BuildCompose
	e.Store.SaveDeployment(ctx, d)
	root, err := safeSubdir(app.Dir, app.RootDir)
	if err != nil {
		return err
	}
	l.Printf("docker compose up (project %s)", e.projectName(app))
	return e.compose(ctx, l, app, root, "up", "-d", "--build", "--remove-orphans")
}

// RebuildFolder refreshes a compose stack in place: newer images for the services that
// use one, a rebuild for those with a build section, then up.
func (e *Engine) RebuildFolder(ctx context.Context, out io.Writer, project, dir, composeFile string) error {
	app := store.App{ID: project, Dir: dir, ComposeFile: composeFile}
	fmt.Fprintf(out, "==> docker compose pull (project %s)\n", project)
	if err := e.compose(ctx, out, app, dir, "pull", "--quiet", "--ignore-buildable"); err != nil {
		fmt.Fprintf(out, "pull failed, going on with the local images: %v\n", err)
	}
	fmt.Fprintln(out, "==> docker compose up -d --build")
	return e.compose(ctx, out, app, dir, "up", "-d", "--build")
}
