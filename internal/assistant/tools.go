package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/beedywool/vps-admin/internal/deploy"
	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/scripts"
	"github.com/beedywool/vps-admin/internal/store"
	"github.com/beedywool/vps-admin/internal/system"
)

const maxResult = 16 << 10

type Deps struct {
	Store     *store.Store
	Runner    *system.Runner
	Docker    *docker.Client
	Collector *system.Collector
	Deploy    *deploy.Engine
	Scripts   *scripts.Runner
	Host      system.Host
	DataDir   string
}

type toolDef struct {
	name, description string
	props             map[string]any
	kind              Kind // fixed kind; run_command is classified per call
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func enum(desc string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": desc}
}

var defs = []toolDef{
	{"server_overview", "Host, OS, uptime, CPU, memory, disks, load and network right now, plus failed services and unhealthy containers.", map[string]any{}, Read},
	{"list_services", "systemd services with their state.", map[string]any{"filter": enum("Which services", "all", "running", "failed")}, Read},
	{"service_logs", "Last journal lines of a systemd service.", map[string]any{"name": str("Service name, without .service"), "lines": num("How many lines, 20 to 500")}, Read},
	{"list_containers", "Docker containers with image, state, health, published ports and compose project.", map[string]any{}, Read},
	{"container_logs", "Last output lines of a Docker container.", map[string]any{"container": str("Container name or id"), "lines": num("How many lines, 20 to 500")}, Read},
	{"security_report", "The panel's security audit: SSH settings, firewall, brute force protection, updates, exposed ports.", map[string]any{}, Read},
	{"list_apps", "Apps deployed from GitHub by the panel, with their live deployment and status.", map[string]any{}, Read},
	{"deployment_log", "Build and start log of an app deployment.", map[string]any{"app": str("App name"), "deployment_id": num("Deployment id, 0 for the latest")}, Read},
	{"list_scripts", "Scripts the administrator linked for the panel (sudo vps-admin link), with their description.", map[string]any{}, Read},
	{"run_script", "Run one of the linked scripts and wait for it (up to 10 minutes). Always needs the administrator's approval.",
		map[string]any{"name": str("Script name, from list_scripts"), "args": str("Arguments, space separated, quotes allowed; empty for none"), "purpose": str("One sentence: why")}, Change},
	{"run_command", "Run a shell command on the server as the unprivileged vps-admin user (sh -c, 60 s timeout). Read-only commands may run right away; anything that changes the system waits for the administrator's approval. `sudo` only works for systemctl start/stop/restart/reload and a few status commands.",
		map[string]any{"command": str("The command line"), "purpose": str("One sentence for the administrator: what this does and why")}, ""},
	{"service_action", "Start, stop, restart or reload a systemd service. Always needs the administrator's approval.",
		map[string]any{"name": str("Service name"), "action": enum("Action", "start", "stop", "restart", "reload"), "purpose": str("One sentence: why, and the expected impact")}, Change},
	{"container_action", "Start, stop or restart a Docker container. Always needs the administrator's approval.",
		map[string]any{"container": str("Container name or id"), "action": enum("Action", "start", "stop", "restart"), "purpose": str("One sentence: why, and the expected impact")}, Change},
	{"deploy_app", "Redeploy an app from its GitHub branch (zero downtime for single-container apps). Always needs the administrator's approval.",
		map[string]any{"app": str("App name"), "purpose": str("One sentence: why")}, Change},
}

func defFor(name string) *toolDef {
	for i := range defs {
		if defs[i].name == name {
			return &defs[i]
		}
	}
	return nil
}

type call struct {
	Filter       string `json:"filter"`
	Name         string `json:"name"`
	Lines        int    `json:"lines"`
	Container    string `json:"container"`
	App          string `json:"app"`
	DeploymentID int64  `json:"deployment_id"`
	Command      string `json:"command"`
	Purpose      string `json:"purpose"`
	Action       string `json:"action"`
	Args         string `json:"args"`
}

var ErrInvalidInput = errors.New("invalid tool input")

// parse validates a tool input. Errors go back to the model as tool errors.
func parse(name, raw string) (*toolDef, *call, error) {
	d := defFor(name)
	if d == nil {
		return nil, nil, fmt.Errorf("%w: unknown tool %s", ErrInvalidInput, name)
	}
	var c call
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	var fields map[string]json.RawMessage
	json.Unmarshal([]byte(raw), &fields)
	for k := range d.props {
		if _, ok := fields[k]; !ok {
			return nil, nil, fmt.Errorf("%w: missing %s", ErrInvalidInput, k)
		}
	}
	return d, &c, nil
}

// kindOf decides how a call is handled, with a one-line summary for the approval card.
func kindOf(d *toolDef, c *call) (Kind, string) {
	switch d.name {
	case "run_command":
		k, why := Classify(c.Command)
		return k, why
	case "service_action":
		return Change, fmt.Sprintf("systemctl %s %s", c.Action, c.Name)
	case "container_action":
		return Change, fmt.Sprintf("docker %s %s", c.Action, c.Container)
	case "deploy_app":
		return Change, "redeploy " + c.App
	case "run_script":
		return Change, strings.TrimSpace("script " + c.Name + " " + c.Args)
	}
	return d.kind, d.name
}

// Display is the short form shown on tool chips and approval cards.
func Display(d *toolDef, c *call) string {
	switch d.name {
	case "run_command":
		return c.Command
	case "service_logs":
		return "journalctl -u " + c.Name
	case "container_logs":
		return "docker logs " + c.Container
	case "deployment_log":
		return "deployment log of " + c.App
	}
	k, s := kindOf(d, c)
	if k == Change {
		return s
	}
	return strings.ReplaceAll(d.name, "_", " ")
}

func clampLines(n int) int { return min(max(n, 20), 500) }

func asJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", " ")
	return string(b)
}

// truncate keeps the start and the end of long outputs, where errors usually are.
func truncate(s string) string {
	if len(s) <= maxResult {
		return s
	}
	head, tail := s[:maxResult/4], s[len(s)-maxResult*3/4:]
	return head + fmt.Sprintf("\n[... %d bytes cut ...]\n", len(s)-maxResult) + tail
}

// execute runs a call and returns the text sent back to the model.
func (dp *Deps) execute(ctx context.Context, d *toolDef, c *call) (string, error) {
	limit := 2 * time.Minute
	if d.name == "run_script" {
		limit = 11 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	switch d.name {
	case "server_overview":
		snap := dp.Collector.Latest()
		res := map[string]any{"host": dp.Host, "snapshot": snap}
		if svcs, err := dp.Runner.Services(ctx); err == nil {
			var failed []string
			for _, s := range svcs {
				if s.Active == "failed" {
					failed = append(failed, s.Name)
				}
			}
			res["failedServices"] = failed
		}
		if cts, err := dp.Docker.Containers(ctx); err == nil {
			var bad []string
			for _, ct := range cts {
				if ct.Health == "unhealthy" || ct.State == "restarting" {
					bad = append(bad, ct.Name+" ("+ct.Status+")")
				}
			}
			res["unhealthyContainers"] = bad
			res["containers"] = len(cts)
		}
		return asJSON(res), nil
	case "list_services":
		svcs, err := dp.Runner.Services(ctx)
		if err != nil {
			return "", err
		}
		var lines []string
		for _, s := range svcs {
			if c.Filter == "running" && s.Active != "active" || c.Filter == "failed" && s.Active != "failed" {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s\t%s/%s\t%s\t%s", s.Name, s.Active, s.Sub, s.Enabled, s.Description))
		}
		return strings.Join(lines, "\n"), nil
	case "service_logs":
		return dp.Runner.ServiceLogs(ctx, c.Name, clampLines(c.Lines))
	case "list_containers":
		cts, err := dp.Docker.Containers(ctx)
		if err != nil {
			return "", err
		}
		return asJSON(cts), nil
	case "container_logs":
		return dp.Docker.Logs(ctx, c.Container, clampLines(c.Lines))
	case "security_report":
		return asJSON(dp.Runner.Audit(ctx)), nil
	case "list_apps":
		apps, err := dp.Store.Apps(ctx)
		if err != nil {
			return "", err
		}
		type view struct {
			store.App
			Status string            `json:"status"`
			Live   *store.Deployment `json:"live,omitempty"`
		}
		out := make([]view, 0, len(apps))
		for _, a := range apps {
			v := view{App: a, Status: dp.Deploy.ContainerStatus(ctx, a)}
			v.Live, _ = dp.Store.LiveDeployment(ctx, a.ID)
			out = append(out, v)
		}
		return asJSON(out), nil
	case "deployment_log":
		id := c.DeploymentID
		if id == 0 {
			deps, err := dp.Store.Deployments(ctx, c.App, 1)
			if err != nil || len(deps) == 0 {
				return "", fmt.Errorf("no deployment for %s", c.App)
			}
			id = deps[0].ID
		}
		d, err := dp.Store.Deployment(ctx, id)
		if err != nil || d.AppID != c.App {
			return "", fmt.Errorf("deployment %d of %s not found", id, c.App)
		}
		log, _ := dp.Store.DeploymentLog(ctx, id)
		return fmt.Sprintf("status: %s\nerror: %s\n\n%s", d.Status, d.Error, log), nil
	case "run_command":
		return dp.shell(ctx, c.Command)
	case "list_scripts":
		list, err := dp.Scripts.List()
		if err != nil {
			return "", err
		}
		return asJSON(list), nil
	case "run_script":
		args, err := scripts.SplitArgs(c.Args)
		if err != nil {
			return "", err
		}
		run, err := dp.Scripts.Start(c.Name, args, func(run *scripts.Run) {
			dp.Store.AddScriptRun(context.WithoutCancel(ctx), run.ID, c.Name, c.Args, "", run.Started)
		}, func(run *scripts.Run) {
			dp.Store.FinishScriptRun(context.WithoutCancel(ctx), run.ID, *run.Finished, run.ExitCode, run.Error, run.Output())
		})
		if err != nil {
			return "", err
		}
		select {
		case <-run.Done():
		case <-time.After(10 * time.Minute):
			return run.Output() + "\n[still running after 10 minutes, it keeps going; see the Scripts page]", nil
		}
		code := -1
		if run.ExitCode != nil {
			code = *run.ExitCode
		}
		return fmt.Sprintf("%s\n[exit status %d]%s", run.Output(), code, map[bool]string{true: " " + run.Error, false: ""}[run.Error != ""]), nil
	case "service_action":
		if err := dp.Runner.ServiceAction(ctx, c.Name, c.Action); err != nil {
			return "", err
		}
		return fmt.Sprintf("systemctl %s %s: done", c.Action, c.Name), nil
	case "container_action":
		if err := dp.Docker.Action(ctx, c.Container, c.Action); err != nil {
			return "", err
		}
		return fmt.Sprintf("docker %s %s: done", c.Action, c.Container), nil
	case "deploy_app":
		app, err := dp.Store.App(ctx, c.App)
		if err != nil {
			return "", fmt.Errorf("app %s not found", c.App)
		}
		d, err := dp.Deploy.Deploy(context.WithoutCancel(ctx), *app, "manual", "", "", "")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Deployment %d started. Follow it with deployment_log (app %s, deployment_id %d).", d.ID, c.App, d.ID), nil
	}
	return "", fmt.Errorf("unknown tool %s", d.name)
}

// shell runs a command as the agent user. sudo becomes `sudo -n` so commands outside the
// allowlist fail instead of waiting for a password.
func (dp *Deps) shell(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if rest, ok := strings.CutPrefix(strings.TrimSpace(command), "sudo "); ok && !strings.HasPrefix(rest, "-n ") {
		command = "sudo -n " + rest
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "HOME=" + dp.DataDir, "LANG=C.UTF-8", "TERM=dumb"}
	cmd.Dir = "/"
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	s := out.String()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return s, errors.New("timed out after 60 s")
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("%s\n[exit status %d]", s, ee.ExitCode()), nil
	}
	if err != nil {
		return s, err
	}
	if s == "" {
		s = "[no output, exit status 0]"
	}
	return s, nil
}
