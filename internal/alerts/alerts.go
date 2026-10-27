// Package alerts posts container failures and full disks to a webhook. It stays quiet
// while a deployment runs and reports recoveries.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beedywool/vps-admin/internal/buildinfo"
	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/store"
	"github.com/beedywool/vps-admin/internal/system"
)

const (
	SettingEnabled = "alerts.enabled"
	SettingWebhook = "alerts.webhook"
	SettingDisk    = "alerts.disk"

	DefaultDisk = 90
	// Below 50% there's nothing to watch; above 99% the alert comes too late.
	DiskMin = 50
	DiskMax = 99

	// Two bad checks in a row are needed, so restarts don't alert.
	every   = 30 * time.Second
	strikes = 2
	// A container created recently that never came up is a failed start.
	fresh = 10 * time.Minute
	// Hysteresis so a disk hovering at the threshold doesn't alert repeatedly.
	hysteresis = 5

	postTimeout = 10 * time.Second
)

// Event is the JSON body posted to the webhook.
type Event struct {
	At     int64  `json:"at"`     // unix seconds
	Kind   string `json:"kind"`   // container | disk | test
	Status string `json:"status"` // down | up | unhealthy | full | freed | test
	Name   string `json:"name,omitempty"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	// Slack reads text, Discord content, ntfy the body and the Title header.
	Text    string `json:"text"`
	Content string `json:"content"`
}

// Config is re-read on every pass, so changes apply without a restart.
type Config struct {
	Enabled bool
	Webhook string
	Disk    int
}

type (
	ContainerLister interface {
		Containers(ctx context.Context) ([]docker.Container, error)
	}
	Snapshotter interface{ Latest() system.Snapshot }
	Deployments interface{ AnyBusy() bool }
)

type entry struct {
	up       bool // seen running at least once since the agent started
	strikes  int
	notified bool
}

type Watcher struct {
	Store   *store.Store
	Docker  ContainerLister
	Metrics Snapshotter
	Deploy  Deployments
	Log     *slog.Logger
	Client  *http.Client

	mu      sync.Mutex
	seen    map[string]*entry
	disks   map[string]bool // mount point -> already alerted
	stopped map[string]bool // container stopped on purpose from the panel or the app
	lastErr string
	lastAt  time.Time
}

// Run checks every 30 seconds. The first pass only learns the state: whatever is already
// down at startup is not an outage.
func (w *Watcher) Run(ctx context.Context) {
	w.init()
	w.Check(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Check(ctx)
		}
	}
}

func (w *Watcher) init() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seen == nil {
		w.seen = map[string]*entry{}
	}
	if w.disks == nil {
		w.disks = map[string]bool{}
	}
	if w.stopped == nil {
		w.stopped = map[string]bool{}
	}
}

// StoppedFromPanel marks a container stopped on purpose: no alert, and no recovery
// notice when it starts again.
func (w *Watcher) StoppedFromPanel(id string) {
	// Docker's list uses short ids.
	if len(id) > 12 {
		id = id[:12]
	}
	w.init()
	w.mu.Lock()
	w.stopped[id] = true
	if e := w.seen[id]; e != nil {
		e.strikes, e.notified = 0, false
	}
	w.mu.Unlock()
}

func (w *Watcher) Config(ctx context.Context) Config { return ReadConfig(ctx, w.Store) }

// ReadConfig is Config for callers without a watcher.
func ReadConfig(ctx context.Context, st *store.Store) Config {
	c := Config{
		Enabled: st.Setting(ctx, SettingEnabled, "true") != "false",
		Webhook: strings.TrimSpace(st.Setting(ctx, SettingWebhook, "")),
		Disk:    DefaultDisk,
	}
	if v, err := strconv.Atoi(st.Setting(ctx, SettingDisk, "")); err == nil && v > 0 {
		c.Disk = v
	}
	return c
}

func (w *Watcher) LastError() (string, time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastErr, w.lastAt
}

func (w *Watcher) ClearError() {
	w.mu.Lock()
	w.lastErr = ""
	w.mu.Unlock()
}

// Check runs one pass. Exported for tests.
func (w *Watcher) Check(ctx context.Context) {
	w.init()
	cfg := w.Config(ctx)
	if !cfg.Enabled || cfg.Webhook == "" {
		return
	}
	var events []Event
	events = append(events, w.checkDisks(cfg)...)
	// Deployments recreate containers on purpose; skip while one runs.
	if w.Deploy != nil && w.Deploy.AnyBusy() {
		w.mu.Lock()
		for _, e := range w.seen {
			e.strikes = 0
		}
		w.mu.Unlock()
		return
	}
	if w.Docker != nil {
		list, err := w.Docker.Containers(ctx)
		if err != nil {
			w.log().Warn("alerts: containers unreadable", "error", err)
			return
		}
		events = append(events, w.checkContainers(list, time.Now())...)
	}
	for _, ev := range events {
		w.Send(ctx, cfg.Webhook, ev)
	}
}

func (w *Watcher) checkContainers(list []docker.Container, now time.Time) []Event {
	var events []Event
	live := make(map[string]bool, len(list))
	w.mu.Lock()
	for _, c := range list {
		live[c.ID] = true
		e := w.seen[c.ID]
		if e == nil {
			e = &entry{up: running(c) || (now.Sub(c.Created) < fresh && c.State != "created")}
			w.seen[c.ID] = e
		}
		bad, title, body := trouble(c)
		if bad && w.stopped[c.ID] {
			continue
		}
		if !bad {
			delete(w.stopped, c.ID)
		}
		switch {
		case bad && e.up:
			e.strikes++
			if e.strikes >= strikes && !e.notified {
				e.notified = true
				events = append(events, Event{Kind: "container", Status: status(c), Name: c.Name, Title: title, Body: body})
			}
		case bad:
			// Never seen up: somebody stopped it long ago, or it was created stopped.
		default:
			if e.notified {
				events = append(events, Event{
					Kind: "container", Status: "up", Name: c.Name,
					Title: c.Name + " is back up", Body: recovered(c),
				})
			}
			e.up, e.strikes, e.notified = true, 0, false
		}
	}
	for id := range w.seen {
		if !live[id] {
			delete(w.seen, id) // removed since the last pass
			delete(w.stopped, id)
		}
	}
	w.mu.Unlock()
	return events
}

func (w *Watcher) checkDisks(cfg Config) []Event {
	if w.Metrics == nil {
		return nil
	}
	var events []Event
	w.mu.Lock()
	for _, d := range w.Metrics.Latest().Disks {
		if d.Total == 0 {
			continue
		}
		pct := 100 * float64(d.Used) / float64(d.Total)
		alerted := w.disks[d.Mount]
		switch {
		case pct >= float64(cfg.Disk) && !alerted:
			w.disks[d.Mount] = true
			events = append(events, Event{
				Kind: "disk", Status: "full", Name: d.Mount,
				Title: fmt.Sprintf("%s is %d%% full", d.Mount, int(pct)),
				Body:  fmt.Sprintf("%s of %s used. Free space is running out.", gib(d.Used), gib(d.Total)),
			})
		case pct <= float64(cfg.Disk-hysteresis) && alerted:
			w.disks[d.Mount] = false
			events = append(events, Event{
				Kind: "disk", Status: "freed", Name: d.Mount,
				Title: fmt.Sprintf("%s has room again", d.Mount),
				Body:  fmt.Sprintf("Down to %d%% used.", int(pct)),
			})
		}
	}
	w.mu.Unlock()
	return events
}

func (w *Watcher) Send(ctx context.Context, hook string, ev Event) {
	err := post(ctx, w.Client, hook, ev)
	w.mu.Lock()
	w.lastAt = time.Now()
	if err != nil {
		w.lastErr = err.Error()
	} else {
		w.lastErr = ""
	}
	w.mu.Unlock()
	entry := store.AuditEntry{Action: "alert.sent", Target: ev.Title, OK: err == nil}
	if err != nil {
		entry.Action, entry.Detail = "alert.failed", err.Error()
	}
	w.Store.Audit(ctx, entry)
	if err != nil {
		w.log().Warn("alerts: webhook failed", "kind", ev.Kind, "name", ev.Name, "error", err)
		return
	}
	w.log().Info("alerts: sent", "kind", ev.Kind, "status", ev.Status, "name", ev.Name)
}

// Test posts a sample event.
func Test(ctx context.Context, client *http.Client, hook string) error {
	return post(ctx, client, hook, Event{
		Kind: "test", Status: "test",
		Title: "VPS Admin test alert",
		Body:  "The webhook works: containers that stop and disks that fill up will arrive here.",
	})
}

// ValidWebhook accepts what net/http can post to: an http or https URL.
func ValidWebhook(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && len(raw) <= 2048
}

func post(ctx context.Context, client *http.Client, hook string, ev Event) error {
	if !ValidWebhook(hook) {
		return fmt.Errorf("not an http or https URL: %q", host(hook))
	}
	if ev.At == 0 {
		ev.At = time.Now().Unix()
	}
	ev.Text = ev.Title + " · " + ev.Body
	ev.Content = ev.Text
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "vps-admin/"+buildinfo.Version)
	// ntfy reads these; every other service ignores them.
	req.Header.Set("Title", ev.Title)
	req.Header.Set("Tags", tags(ev))
	if ev.Status == "down" || ev.Status == "unhealthy" || ev.Status == "full" {
		req.Header.Set("Priority", "high")
	}
	if client == nil {
		client = &http.Client{Timeout: postTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s answered HTTP %d", host(hook), resp.StatusCode)
	}
	return nil
}

func host(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

func tags(ev Event) string {
	if ev.Status == "up" || ev.Status == "freed" {
		return "white_check_mark"
	}
	return "warning"
}

func running(c docker.Container) bool {
	return c.State == "running" || c.State == "restarting"
}

func trouble(c docker.Container) (bool, string, string) {
	status := strings.TrimSpace(c.Status)
	if status == "" {
		status = c.State
	}
	where := ""
	if c.Project != "" {
		where = " · project " + c.Project
	}
	if c.State == "running" && c.Health == "unhealthy" {
		return true, c.Name + " is unhealthy", "Its healthcheck keeps failing (" + status + ")" + where + "."
	}
	if c.State != "running" {
		return true, c.Name + " is down", "Docker reports " + strings.ToLower(status) + where + "."
	}
	return false, "", ""
}

func status(c docker.Container) string {
	if c.State == "running" && c.Health == "unhealthy" {
		return "unhealthy"
	}
	return "down"
}

func recovered(c docker.Container) string {
	if c.Health == "healthy" {
		return "Its healthcheck passes again."
	}
	return "Docker reports it running again."
}

func gib(n uint64) string {
	return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
}

func (w *Watcher) log() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}
