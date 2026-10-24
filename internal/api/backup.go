package api

import (
	"net/http"
	"time"

	"github.com/beedywool/vps-admin/internal/backup"
)

// Backup to Google Drive. The settings page configures the rclone remote, mounts it
// through the root helper, and starts or restores an encrypted archive.

func (s *Server) backupReady(w http.ResponseWriter) bool {
	if s.Backup == nil {
		writeErrorMsg(w, http.StatusServiceUnavailable, "unavailable", "backup is not configured on this agent")
		return false
	}
	return true
}

func (s *Server) backupStatus(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.Backup.Status(r.Context(), r.URL.Query().Get("archives") == "1"))
}

func (s *Server) backupUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	var o backup.Options
	if err := readJSON(r, &o); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	err := s.Backup.Save(r.Context(), o)
	s.audit(r, "backup.settings", "", err)
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	s.backupStatus(w, r)
}

func (s *Server) backupTest(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	msg, err := s.Backup.Test(r.Context())
	s.audit(r, "backup.test", "", err)
	if err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "backup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "detail": msg})
}

func (s *Server) backupMount(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	var body struct {
		Mounted bool `json:"mounted"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	err := s.Backup.SetMount(r.Context(), body.Mounted)
	s.audit(r, "backup.mount", map[bool]string{true: "mount", false: "umount"}[body.Mounted], err)
	if err != nil {
		writeErrorMsg(w, http.StatusConflict, "mount_failed", err.Error())
		return
	}
	s.backupStatus(w, r)
}

func (s *Server) backupRun(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	var body struct {
		Kind string `json:"kind"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if body.Kind == "" {
		body.Kind = "settings"
	}
	err := s.Backup.Start(r.Context(), body.Kind)
	s.audit(r, "backup.run", body.Kind, err)
	if err != nil {
		writeErrorMsg(w, http.StatusConflict, "backup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

// backupRestore stages an archive's database and restarts the agent, which swaps the
// files in before opening the store. The panel is unreachable for a few seconds.
func (s *Server) backupRestore(w http.ResponseWriter, r *http.Request) {
	if !s.backupReady(w) {
		return
	}
	var body struct {
		File       string `json:"file"`
		Passphrase string `json:"passphrase"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	h, err := s.Backup.Restore(r.Context(), body.File, body.Passphrase)
	s.audit(r, "backup.restore", body.File, err)
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "restore_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"restarting": true, "created": h.Created, "host": h.Host})
	if s.Restart != nil {
		// Let the response reach the panel before the process stops.
		go func() {
			time.Sleep(500 * time.Millisecond)
			s.Restart()
		}()
	}
}
