package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beedywool/vps-admin/internal/alerts"
	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/docker"
	"github.com/beedywool/vps-admin/internal/store"
)

func alertServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Cfg: cfg, Store: st}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %v", rec.Body.String(), err)
	}
	return out
}

func TestAlertsDefaults(t *testing.T) {
	s := alertServer(t)
	rec := httptest.NewRecorder()
	s.alertsGet(rec, httptest.NewRequest("GET", "/api/alerts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := decode(t, rec)
	if got["enabled"] != true || got["disk"] != float64(90) || got["webhook"] != "" {
		t.Fatalf("defaults = %v", got)
	}
}

func TestAlertsRoundTrip(t *testing.T) {
	s := alertServer(t)
	body := `{"enabled":false,"webhook":"https://ntfy.sh/my-topic","disk":80}`
	rec := httptest.NewRecorder()
	s.alertsPut(rec, httptest.NewRequest("PUT", "/api/alerts", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	got := decode(t, rec)
	if got["enabled"] != false || got["webhook"] != "https://ntfy.sh/my-topic" || got["disk"] != float64(80) {
		t.Fatalf("saved = %v", got)
	}
	// Stored, not just echoed: a second read finds the same.
	rec = httptest.NewRecorder()
	s.alertsGet(rec, httptest.NewRequest("GET", "/api/alerts", nil))
	if again := decode(t, rec); again["disk"] != float64(80) || again["enabled"] != false {
		t.Fatalf("read back = %v", again)
	}
}

func TestAlertsRejectsBadInput(t *testing.T) {
	s := alertServer(t)
	for _, body := range []string{
		`{"webhook":"ntfy.sh/no-scheme"}`,
		`{"webhook":"file:///etc/passwd"}`,
		`{"disk":10}`,
		`{"disk":100}`,
		`{"disk":"ninety"}`,
	} {
		rec := httptest.NewRecorder()
		s.alertsPut(rec, httptest.NewRequest("PUT", "/api/alerts", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestAlertsTestButton(t *testing.T) {
	var got map[string]any
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()

	s := alertServer(t)
	rec := httptest.NewRecorder()
	s.alertsTest(rec, httptest.NewRequest("POST", "/api/alerts/test",
		strings.NewReader(fmt.Sprintf(`{"webhook":%q}`, hook.URL))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got["kind"] != "test" || got["title"] != "VPS Admin test alert" {
		t.Fatalf("posted = %v", got)
	}
}

func TestAlertsTestReportsTheWebhookAnswer(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer hook.Close()

	s := alertServer(t)
	rec := httptest.NewRecorder()
	s.alertsTest(rec, httptest.NewRequest("POST", "/api/alerts/test",
		strings.NewReader(fmt.Sprintf(`{"webhook":%q}`, hook.URL))))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if msg := decode(t, rec)["message"]; msg == nil || !strings.Contains(fmt.Sprint(msg), "HTTP 500") {
		t.Fatalf("message = %v", msg)
	}
}

// fakeDocker serves the two Docker calls the stop path and the watcher need, on a unix
// socket, so the real client is exercised.
type fakeDocker struct {
	mu    sync.Mutex
	state string
}

func (f *fakeDocker) set(state string) {
	f.mu.Lock()
	f.state = state
	f.mu.Unlock()
}

func (f *fakeDocker) socket(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.41/containers/json", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		state := f.state
		f.mu.Unlock()
		status := "Exited (0) 2 seconds ago"
		if state == "running" {
			status = "Up 2 hours"
		}
		json.NewEncoder(w).Encode([]map[string]any{{
			"Id": "abcdef0123456789", "Names": []string{"/web"}, "Image": "nginx:1",
			"State": state, "Status": status, "Created": time.Now().Add(-time.Hour).Unix(),
			"Labels": map[string]string{}, "Ports": []any{},
		}})
	})
	mux.HandleFunc("/v1.41/containers/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return path
}

// A container stopped from the panel is not an outage: the handler marks it and the
// watcher stays quiet, through the real Docker client.
func TestStoppingFromThePanelDoesNotAlert(t *testing.T) {
	var mu sync.Mutex
	var sent int
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sent++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SetSetting(ctx, alerts.SettingWebhook, hook.URL); err != nil {
		t.Fatal(err)
	}

	dock := &fakeDocker{state: "running"}
	cli := docker.New(dock.socket(t))
	watcher := &alerts.Watcher{Store: st, Docker: cli}
	watcher.Check(ctx) // learns the container is up

	s := &Server{Cfg: mustConfig(t), Store: st, Docker: cli, Alerts: watcher}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/docker/containers/abcdef012345/stop", nil)
	req.SetPathValue("id", "abcdef012345")
	req.SetPathValue("action", "stop")
	s.containerAction(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("stop = %d: %s", rec.Code, rec.Body.String())
	}

	dock.set("exited")
	watcher.Check(ctx)
	watcher.Check(ctx)
	watcher.Check(ctx)
	if n := count(&mu, &sent); n != 0 {
		t.Fatalf("a container stopped from the panel sent %d alert(s)", n)
	}

	// The same crash without the panel mark does alert: the test would see it.
	dock.set("running")
	watcher.Check(ctx)
	dock.set("exited")
	watcher.Check(ctx)
	watcher.Check(ctx)
	if n := count(&mu, &sent); n != 1 {
		t.Fatalf("a real crash sent %d alert(s), want 1", n)
	}
}

func count(mu *sync.Mutex, n *int) int {
	mu.Lock()
	defer mu.Unlock()
	return *n
}

func mustConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAlertsTestWithoutWebhook(t *testing.T) {
	s := alertServer(t)
	rec := httptest.NewRecorder()
	s.alertsTest(rec, httptest.NewRequest("POST", "/api/alerts/test", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
