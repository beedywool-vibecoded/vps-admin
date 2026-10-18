package system

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	Name        string `json:"name"` // without .service
	Description string `json:"description"`
	Load        string `json:"load"`   // loaded | not-found | masked
	Active      string `json:"active"` // active | inactive | failed | activating
	Sub         string `json:"sub"`    // running | exited | dead | ...
	Enabled     string `json:"enabled,omitempty"`
}

var (
	unitName       = regexp.MustCompile(`^[A-Za-z0-9@._:\\-]{1,200}$`)
	ErrBadUnit     = errors.New("invalid service name")
	ErrBadAction   = errors.New("action must be start, stop, restart or reload")
	ErrSelfLockout = errors.New("stopping this service would cut your access to the server")
)

// lockout lists services that must never be stopped from the panel: losing them means
// losing the way back in. Restarting them stays allowed.
var lockout = []string{"ssh", "sshd", "vps-admin"}

func (r *Runner) Services(ctx context.Context) ([]Service, error) {
	out, err := r.Run(ctx, 15*time.Second, "systemctl", "list-units", "--type=service", "--all", "--plain", "--no-legend", "--no-pager")
	if err != nil {
		return nil, err
	}
	enabled := map[string]string{}
	if files, err := r.Run(ctx, 15*time.Second, "systemctl", "list-unit-files", "--type=service", "--plain", "--no-legend", "--no-pager"); err == nil {
		for _, l := range strings.Split(files, "\n") {
			if f := strings.Fields(l); len(f) >= 2 {
				enabled[strings.TrimSuffix(f[0], ".service")] = f[1]
			}
		}
	}
	var list []Service
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 4 || !strings.HasSuffix(f[0], ".service") {
			continue
		}
		s := Service{Name: strings.TrimSuffix(f[0], ".service"), Load: f[1], Active: f[2], Sub: f[3]}
		if len(f) > 4 {
			s.Description = strings.Join(f[4:], " ")
		}
		if s.Load == "not-found" {
			continue
		}
		s.Enabled = enabled[s.Name]
		list = append(list, s)
	}
	slices.SortFunc(list, func(a, b Service) int { return strings.Compare(a.Name, b.Name) })
	return list, nil
}

func validUnit(name string) bool {
	return unitName.MatchString(name) && !strings.HasPrefix(name, "-")
}

// ServiceAction starts, stops, restarts or reloads a service.
func (r *Runner) ServiceAction(ctx context.Context, name, action string) error {
	name = strings.TrimSuffix(name, ".service")
	if !validUnit(name) {
		return ErrBadUnit
	}
	if !slices.Contains([]string{"start", "stop", "restart", "reload"}, action) {
		return ErrBadAction
	}
	if action == "stop" && slices.Contains(lockout, name) {
		return ErrSelfLockout
	}
	_, err := r.Sudo(ctx, 90*time.Second, "systemctl", action, name+".service")
	return err
}

// ServiceLogs returns the last lines of the unit journal. The agent user is in the
// systemd-journal group, so no sudo is needed.
func (r *Runner) ServiceLogs(ctx context.Context, name string, lines int) (string, error) {
	name = strings.TrimSuffix(name, ".service")
	if !validUnit(name) {
		return "", ErrBadUnit
	}
	lines = min(max(lines, 1), 5000)
	return r.Run(ctx, 20*time.Second, "journalctl", "-u", name+".service", "-n", strconv.Itoa(lines), "--no-pager", "-o", "short-iso")
}
