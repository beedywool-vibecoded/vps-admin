package alerts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/store"
	"github.com/beedywool/vps-admin/internal/system"
)

type fakeDocker struct {
	mu   sync.Mutex
	list []docker.Container
}

func (f *fakeDocker) Containers(context.Context) ([]docker.Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]docker.Container(nil), f.list...), nil
}

func (f *fakeDocker) set(list ...docker.Container) {
	f.mu.Lock()
	f.list = list
	f.mu.Unlock()
}

type fakeMetrics struct{ disks []system.Disk }

func (f *fakeMetrics) Latest() system.Snapshot { return system.Snapshot{Disks: f.disks} }

type fakeDeploy struct{ busy bool }

func (f *fakeDeploy) AnyBusy() bool { return f.busy }

// hook records what a webhook would have received.
type hook struct {
	mu     sync.Mutex
	events []Event
	req    *http.Request
	status int
	srv    *httptest.Server
}

func newHook(t *testing.T) *hook {
	t.Helper()
	h := &hook{status: http.StatusNoContent}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev Event
		json.NewDecoder(r.Body).Decode(&ev)
		h.mu.Lock()
		h.events = append(h.events, ev)
		h.req = r
		status := h.status
		h.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *hook) got() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Event(nil), h.events...)
}

func newWatcher(t *testing.T, h *hook) (*Watcher, *fakeDocker, *fakeMetrics, *fakeDeploy) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	if err := st.SetSetting(ctx, SettingWebhook, h.srv.URL); err != nil {
		t.Fatal(err)
	}
	d, m, dep := &fakeDocker{}, &fakeMetrics{}, &fakeDeploy{}
	return &Watcher{Store: st, Docker: d, Metrics: m, Deploy: dep}, d, m, dep
}

func container(name, state string) docker.Container {
	return docker.Container{
		ID: name, Name: name, Image: "img:" + name, State: state,
		Status: statusText(state), Created: time.Now().Add(-24 * time.Hour),
	}
}

func statusText(state string) string {
	if state == "running" {
		return "Up 2 hours"
	}
	return "Exited (1) 2 seconds ago"
}

func TestDownThenBack(t *testing.T) {
	h := newHook(t)
	w, d, _, _ := newWatcher(t, h)
	ctx := context.Background()

	d.set(container("web", "running"))
	w.Check(ctx) // learns it is up, says nothing
	if len(h.got()) != 0 {
		t.Fatalf("first pass alerted: %+v", h.got())
	}

	d.set(container("web", "exited"))
	w.Check(ctx)
	if len(h.got()) != 0 {
		t.Fatal("a single bad check is a restart, not an outage")
	}
	w.Check(ctx)
	got := h.got()
	if len(got) != 1 || got[0].Kind != "container" || got[0].Status != "down" || got[0].Name != "web" {
		t.Fatalf("want one down alert, got %+v", got)
	}
	if got[0].Title != "web is down" || got[0].Body == "" {
		t.Fatalf("bad wording: %+v", got)
	}

	w.Check(ctx) // still down: stay quiet
	if len(h.got()) != 1 {
		t.Fatalf("down was reported twice: %+v", h.got())
	}

	d.set(container("web", "running"))
	w.Check(ctx)
	got = h.got()
	if len(got) != 2 || got[1].Status != "up" || got[1].Name != "web" {
		t.Fatalf("want a recovery alert, got %+v", got)
	}

	// Down again later: a new outage deserves a new alert.
	d.set(container("web", "exited"))
	w.Check(ctx)
	w.Check(ctx)
	if len(h.got()) != 3 {
		t.Fatalf("second outage missed: %+v", h.got())
	}
}

func TestStoppedLongAgoIsSilent(t *testing.T) {
	h := newHook(t)
	w, d, _, _ := newWatcher(t, h)
	ctx := context.Background()
	d.set(container("old", "exited")) // created a day ago, someone turned it off
	for i := 0; i < 4; i++ {
		w.Check(ctx)
	}
	if got := h.got(); len(got) != 0 {
		t.Fatalf("an app stopped for good alerted: %+v", got)
	}
}

func TestFreshContainerThatNeverCameUp(t *testing.T) {
	h := newHook(t)
	w, d, _, _ := newWatcher(t, h)
	ctx := context.Background()
	// Started by a deployment a minute ago and already dead.
	d.set(docker.Container{
		ID: "new", Name: "new", State: "exited", Status: "Exited (1) 30 seconds ago",
		Created: time.Now().Add(-time.Minute),
	})
	w.Check(ctx)
	w.Check(ctx)
	if got := h.got(); len(got) != 1 || got[0].Status != "down" {
		t.Fatalf("a failed start went unnoticed: %+v", got)
	}
}

func TestUnhealthy(t *testing.T) {
	h := newHook(t)
	w, d, _, _ := newWatcher(t, h)
	ctx := context.Background()
	d.set(container("api", "running"))
	w.Check(ctx)

	bad := container("api", "running")
	bad.Health = "unhealthy"
	bad.Status = "Up 2 hours (unhealthy)"
	d.set(bad)
	w.Check(ctx)
	w.Check(ctx)
	got := h.got()
	if len(got) != 1 || got[0].Status != "unhealthy" {
		t.Fatalf("want one unhealthy alert, got %+v", got)
	}

	d.set(container("api", "running"))
	w.Check(ctx)
	if len(h.got()) != 2 || h.got()[1].Status != "up" {
		t.Fatalf("recovery missed: %+v", h.got())
	}
}

func TestQuietDuringDeployment(t *testing.T) {
	h := newHook(t)
	w, d, _, dep := newWatcher(t, h)
	ctx := context.Background()
	d.set(container("app", "running"))
	w.Check(ctx)

	// A deployment stops the container and recreates it: nothing may be said.
	dep.busy = true
	d.set(container("app", "exited"))
	for i := 0; i < 4; i++ {
		w.Check(ctx)
	}
	if got := h.got(); len(got) != 0 {
		t.Fatalf("a deployment alerted: %+v", got)
	}

	// The deployment ends and the app comes back: still nothing, it never went down.
	dep.busy = false
	d.set(container("app", "running"))
	w.Check(ctx)
	if got := h.got(); len(got) != 0 {
		t.Fatalf("a deployment recovery alerted: %+v", got)
	}

	// After that, a real outage is reported again.
	d.set(container("app", "exited"))
	w.Check(ctx)
	w.Check(ctx)
	if len(h.got()) != 1 {
		t.Fatalf("alerts stayed off after the deployment: %+v", h.got())
	}
}

func TestManualStopIsSilent(t *testing.T) {
	h := newHook(t)
	w, d, _, _ := newWatcher(t, h)
	ctx := context.Background()
	d.set(container("web", "running"))
	w.Check(ctx)

	// Stopped from the panel or the app: down is what was asked for.
	w.StoppedFromPanel("web")
	d.set(container("web", "exited"))
	for i := 0; i < 4; i++ {
		w.Check(ctx)
	}
	if got := h.got(); len(got) != 0 {
		t.Fatalf("a deliberate stop alerted: %+v", got)
	}

	// Started again: no "back up" either, the admin knows they started it.
	d.set(container("web", "running"))
	w.Check(ctx)
	if got := h.got(); len(got) != 0 {
		t.Fatalf("a deliberate start alerted: %+v", got)
	}

	// Then it really crashes: that is an outage again.
	d.set(container("web", "exited"))
	w.Check(ctx)
	w.Check(ctx)
	if got := h.got(); len(got) != 1 || got[0].Status != "down" {
		t.Fatalf("a real outage after a manual stop went unnoticed: %+v", got)
	}
}

func TestAlertsLandInActivity(t *testing.T) {
	h := newHook(t)
	w, d, _, _ := newWatcher(t, h)
	ctx := context.Background()
	d.set(container("web", "running"))
	w.Check(ctx)
	d.set(container("web", "exited"))
	w.Check(ctx)
	w.Check(ctx)

	entries, err := w.Store.AuditLog(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Action != "alert.sent" || entries[0].Target != "web is down" || !entries[0].OK {
		t.Fatalf("activity = %+v", entries)
	}

	// A delivery that fails is kept too, with what the service answered.
	h.mu.Lock()
	h.status = http.StatusInternalServerError
	h.mu.Unlock()
	d.set(container("web", "running"))
	w.Check(ctx)
	entries, err = w.Store.AuditLog(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Action != "alert.failed" || entries[0].OK || !strings.Contains(entries[0].Detail, "HTTP 500") {
		t.Fatalf("failed delivery not in activity: %+v", entries[0])
	}
}

func TestDiskThreshold(t *testing.T) {
	h := newHook(t)
	w, _, m, _ := newWatcher(t, h)
	ctx := context.Background()
	disk := func(usedGB uint64) []system.Disk {
		return []system.Disk{{Mount: "/", Total: 100 << 30, Used: usedGB << 30}}
	}

	m.disks = disk(92)
	w.Check(ctx)
	if got := h.got(); len(got) != 1 || got[0].Kind != "disk" || got[0].Status != "full" {
		t.Fatalf("want a full disk alert, got %+v", got)
	}

	m.disks = disk(93)
	w.Check(ctx)
	if len(h.got()) != 1 {
		t.Fatalf("the same full disk was reported twice: %+v", h.got())
	}

	// Just under the threshold is not enough to call it fixed.
	m.disks = disk(88)
	w.Check(ctx)
	if len(h.got()) != 1 {
		t.Fatalf("no recovery before the hysteresis: %+v", h.got())
	}

	m.disks = disk(84)
	w.Check(ctx)
	if got := h.got(); len(got) != 2 || got[1].Status != "freed" {
		t.Fatalf("want a freed alert, got %+v", got)
	}
}

func TestThresholdIsConfigurable(t *testing.T) {
	h := newHook(t)
	w, _, m, _ := newWatcher(t, h)
	ctx := context.Background()
	if err := w.Store.SetSetting(ctx, SettingDisk, "70"); err != nil {
		t.Fatal(err)
	}
	m.disks = []system.Disk{{Mount: "/data", Total: 100 << 30, Used: 71 << 30}}
	w.Check(ctx)
	if got := h.got(); len(got) != 1 || got[0].Name != "/data" {
		t.Fatalf("the configured threshold was ignored: %+v", got)
	}
}

func TestDisabledSaysNothing(t *testing.T) {
	h := newHook(t)
	w, d, m, _ := newWatcher(t, h)
	ctx := context.Background()
	if err := w.Store.SetSetting(ctx, SettingEnabled, "false"); err != nil {
		t.Fatal(err)
	}
	d.set(container("web", "running"))
	m.disks = []system.Disk{{Mount: "/", Total: 100 << 30, Used: 99 << 30}}
	w.Check(ctx)
	d.set(container("web", "exited"))
	w.Check(ctx)
	w.Check(ctx)
	if got := h.got(); len(got) != 0 {
		t.Fatalf("disabled alerts were sent: %+v", got)
	}
}

func TestPayloadAndHeaders(t *testing.T) {
	h := newHook(t)
	w, d, _, _ := newWatcher(t, h)
	ctx := context.Background()
	d.set(container("web", "running"))
	w.Check(ctx)
	d.set(container("web", "exited"))
	w.Check(ctx)
	w.Check(ctx)

	var raw map[string]any
	b, _ := json.Marshal(h.got()[0])
	json.Unmarshal(b, &raw)
	for _, key := range []string{"at", "kind", "status", "name", "title", "body", "text", "content"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("payload has no %q: %s", key, b)
		}
	}
	if raw["text"] != "web is down · Docker reports exited (1) 2 seconds ago." {
		t.Errorf("text = %v", raw["text"])
	}
	h.mu.Lock()
	req := h.req
	h.mu.Unlock()
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q", got)
	}
	if got := req.Header.Get("Title"); got != "web is down" {
		t.Errorf("ntfy title header = %q", got)
	}
	if got := req.Header.Get("Priority"); got != "high" {
		t.Errorf("priority = %q", got)
	}
}

func TestFailedWebhookIsRemembered(t *testing.T) {
	h := newHook(t)
	h.status = http.StatusNotFound
	w, d, _, _ := newWatcher(t, h)
	ctx := context.Background()
	d.set(container("web", "running"))
	w.Check(ctx)
	d.set(container("web", "exited"))
	w.Check(ctx)
	w.Check(ctx)
	msg, _ := w.LastError()
	if msg == "" {
		t.Fatal("a failing webhook left no error for the settings page")
	}
	// Fixing the URL clears what the old one reported.
	w.ClearError()
	if msg, _ := w.LastError(); msg != "" {
		t.Fatalf("the error survived a fix: %s", msg)
	}
}

func TestTestAlert(t *testing.T) {
	h := newHook(t)
	if err := Test(context.Background(), nil, h.srv.URL); err != nil {
		t.Fatal(err)
	}
	got := h.got()
	if len(got) != 1 || got[0].Kind != "test" || got[0].Title != "VPS Admin test alert" {
		t.Fatalf("unexpected test event: %+v", got)
	}
}

func TestValidWebhook(t *testing.T) {
	for _, ok := range []string{"https://ntfy.sh/topic", "http://10.0.0.2:8080/hook"} {
		if !ValidWebhook(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"", "ntfy.sh/topic", "file:///etc/passwd", "https://"} {
		if ValidWebhook(bad) {
			t.Errorf("%q should be refused", bad)
		}
	}
}
