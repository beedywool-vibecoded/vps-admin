package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beedywool/vps-admin/internal/backup"
	"github.com/beedywool/vps-admin/internal/config"
	"github.com/beedywool/vps-admin/internal/secret"
	"github.com/beedywool/vps-admin/internal/store"
)

func backupServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, err := secret.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load("/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	cfg.DataDir = dir
	s := &Server{Store: st, Cfg: cfg, Box: box}
	s.Backup = &backup.Manager{Store: st, Box: box, Cfg: cfg, Version: "test"}
	return s
}

func TestBackupSettingsRoundTrip(t *testing.T) {
	s := backupServer(t)
	rec := httptest.NewRecorder()
	s.backupStatus(rec, httptest.NewRequest("GET", "/api/backup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if got := decode(t, rec); got["configured"] != false || got["hasToken"] != false {
		t.Fatalf("a fresh agent must report no Drive: %v", got)
	}

	body, _ := json.Marshal(map[string]any{
		"token":      `{"access_token":"a","token_type":"Bearer","refresh_token":"r"}`,
		"passphrase": "a long enough passphrase",
		"path":       "backups/vps-admin",
		"hour":       4,
		"keep":       3,
		"kind":       "full",
		"enabled":    true,
	})
	req := httptest.NewRequest("PUT", "/api/backup", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	s.backupUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	got := decode(t, rec)
	for _, k := range []string{"configured", "hasToken", "hasPassphrase"} {
		if got[k] != true {
			t.Errorf("%s should be true after saving: %v", k, got)
		}
	}
	if got["path"] != "backups/vps-admin" || got["hour"] != float64(4) || got["keep"] != float64(3) {
		t.Fatalf("the saved values did not come back: %v", got)
	}
	if got["kind"] != "full" {
		t.Fatalf("the daily kind did not come back: %v", got)
	}

	// The token and the passphrase are never echoed back.
	if _, leaked := got["token"]; leaked {
		t.Fatal("the token must not be returned")
	}
}

func TestBackupRefusesBadSettings(t *testing.T) {
	s := backupServer(t)
	for _, body := range []string{
		`{"hour":24}`,
		`{"keep":0}`,
		`{"path":"../etc"}`,
		`{"passphrase":"short"}`,
		`{"token":"not json"}`,
		`{"kind":"everything"}`,
	} {
		rec := httptest.NewRecorder()
		s.backupUpdate(rec, httptest.NewRequest("PUT", "/api/backup", bytes.NewReader([]byte(body))))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
}

func TestBackupDeleteRefusesAnUnknownFile(t *testing.T) {
	s := backupServer(t)
	rec := httptest.NewRecorder()
	s.backupDelete(rec, httptest.NewRequest("POST", "/api/backup/delete",
		bytes.NewReader([]byte(`{"file":"../../etc/passwd"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestBackupRestoreRefusesAnUnknownFile(t *testing.T) {
	s := backupServer(t)
	rec := httptest.NewRecorder()
	s.backupRestore(rec, httptest.NewRequest("POST", "/api/backup/restore",
		bytes.NewReader([]byte(`{"file":"../../etc/passwd","passphrase":"a long enough passphrase"}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestBackupRunNeedsAConnection(t *testing.T) {
	s := backupServer(t)
	rec := httptest.NewRecorder()
	s.backupRun(rec, httptest.NewRequest("POST", "/api/backup/run", bytes.NewReader([]byte(`{"kind":"settings"}`))))
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d (%s)", rec.Code, rec.Body.String())
	}
}
