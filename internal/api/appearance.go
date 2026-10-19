package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// The server background: one image chosen by an administrator, shown to everyone on
// the panel and in the app, with an opacity. Without one the background stays plain.

const maxBackground = 8 << 20

var backgroundTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

func (s *Server) backgroundPath() string { return filepath.Join(s.Cfg.DataDir, "background") }

func (s *Server) appearance(w http.ResponseWriter, r *http.Request) {
	res := map[string]any{"background": false, "opacity": s.opacity(r), "version": ""}
	if st, err := os.Stat(s.backgroundPath()); err == nil {
		res["background"] = true
		res["version"] = strconv.FormatInt(st.ModTime().Unix(), 36) // cache buster
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) opacity(r *http.Request) float64 {
	v, err := strconv.ParseFloat(s.Store.Setting(r.Context(), "appearance.opacity", "0.35"), 64)
	if err != nil {
		return 0.35
	}
	return v
}

func (s *Server) setAppearance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Opacity *float64 `json:"opacity"`
	}
	if err := readJSON(r, &body); err != nil || body.Opacity == nil || *body.Opacity < 0 || *body.Opacity > 1 {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "opacity must be between 0 and 1")
		return
	}
	err := s.Store.SetSetting(r.Context(), "appearance.opacity", strconv.FormatFloat(*body.Opacity, 'f', 2, 64))
	s.audit(r, "appearance.opacity", strconv.FormatFloat(*body.Opacity, 'f', 2, 64), err)
	s.appearance(w, r)
}

func (s *Server) backgroundGet(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open(s.backgroundPath())
	if err != nil {
		writeError(w, http.StatusNotFound, "no_background")
		return
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	f.Seek(0, io.SeekStart)
	st, _ := f.Stat()
	w.Header().Set("Content-Type", http.DetectContentType(head[:n]))
	// Versioned URL (?v=...), so it can be cached for long.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeContent(w, r, "background", st.ModTime(), f)
}

// backgroundPut takes the raw image as the request body.
func (s *Server) backgroundPut(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBackground+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if len(data) > maxBackground {
		writeErrorMsg(w, http.StatusRequestEntityTooLarge, "too_large", "the image must be 8 MB or less")
		return
	}
	if ct := http.DetectContentType(data); !backgroundTypes[ct] {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "use a JPEG, PNG or WebP image")
		return
	}
	tmp := s.backgroundPath() + ".tmp"
	err = os.WriteFile(tmp, data, 0o600)
	if err == nil {
		err = os.Rename(tmp, s.backgroundPath())
	}
	os.Chtimes(s.backgroundPath(), time.Now(), time.Now())
	s.audit(r, "appearance.background", strconv.Itoa(len(data))+" bytes", err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	s.appearance(w, r)
}

func (s *Server) backgroundDelete(w http.ResponseWriter, r *http.Request) {
	err := os.Remove(s.backgroundPath())
	if os.IsNotExist(err) {
		err = nil
	}
	s.audit(r, "appearance.background_removed", "", err)
	s.appearance(w, r)
}
