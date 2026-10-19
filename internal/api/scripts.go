package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/beedywool/vps-admin/internal/auth"
	"github.com/beedywool/vps-admin/internal/scripts"
)

func (s *Server) scriptsList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Scripts.List()
	if err != nil {
		writeErrorMsg(w, http.StatusInternalServerError, "scripts_failed", err.Error())
		return
	}
	titles := s.scriptTitles(r.Context())
	for i := range list {
		list[i].Title = titles[list[i].Name]
	}
	runs, _ := s.Store.ScriptRuns(r.Context(), "", 50)
	writeJSON(w, http.StatusOK, map[string]any{"scripts": list, "runs": runs, "dir": s.Cfg.ScriptsDir})
}

const scriptTitlesSetting = "scripts.titles"

// scriptTitles maps link names to the display names given in the panel.
func (s *Server) scriptTitles(ctx context.Context) map[string]string {
	m := map[string]string{}
	json.Unmarshal([]byte(s.Store.Setting(ctx, scriptTitlesSetting, "{}")), &m)
	return m
}

// scriptRename sets the display name of a script; an empty title goes back to the link
// name. The link itself keeps its name, so the CLI and the history are unchanged.
func (s *Server) scriptRename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	name := r.PathValue("name")
	title := auth.CleanName(body.Title, "")
	titles := s.scriptTitles(r.Context())
	if title == "" || title == name {
		delete(titles, name)
	} else {
		titles[name] = title
	}
	b, _ := json.Marshal(titles)
	err := s.Store.SetSetting(r.Context(), scriptTitlesSetting, string(b))
	s.audit(r, "script.rename", name, err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) scriptRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Args string `json:"args"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	args, err := scripts.SplitArgs(body.Args)
	if err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	name := r.PathValue("name")
	dev := auth.FromContext(r.Context()).DeviceID
	ctx := context.WithoutCancel(r.Context())
	run, err := s.Scripts.Start(name, args, func(run *scripts.Run) {
		s.Store.AddScriptRun(ctx, run.ID, name, body.Args, dev, run.Started)
	}, func(run *scripts.Run) {
		s.Store.FinishScriptRun(ctx, run.ID, *run.Finished, run.ExitCode, run.Error, run.Output())
	})
	s.audit(r, "script.run", strings.TrimSpace(name+" "+body.Args), err)
	switch {
	case errors.Is(err, scripts.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
		return
	case err != nil:
		writeErrorMsg(w, http.StatusConflict, "run_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": run.ID})
}

func (s *Server) scriptStop(w http.ResponseWriter, r *http.Request) {
	ok := s.Scripts.Stop(r.PathValue("name"))
	s.audit(r, "script.stop", r.PathValue("name"), nil)
	if !ok {
		writeError(w, http.StatusConflict, "not_running")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) scriptRunOutput(w http.ResponseWriter, r *http.Request) {
	out, err := s.Store.ScriptRunOutput(r.Context(), r.PathValue("run"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"output": out})
}

// scriptStream follows the run in progress of a script: output so far, then live
// chunks, then an "end" event with the exit code.
func (s *Server) scriptStream(w http.ResponseWriter, r *http.Request) {
	run := s.Scripts.Current(r.PathValue("name"))
	if run == nil {
		writeError(w, http.StatusNotFound, "not_running")
		return
	}
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	out, ch, cancel := run.Follow()
	defer cancel()
	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("output", out) {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case chunk := <-ch:
			if !send("output", chunk) {
				return
			}
		case <-run.Done():
			for {
				select {
				case chunk := <-ch:
					send("output", chunk)
					continue
				default:
				}
				break
			}
			send("end", map[string]any{"exitCode": run.ExitCode, "error": run.Error})
			return
		}
	}
}

// reboot restarts the whole server, after a short delay so the answer gets out.
func (s *Server) reboot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm string `json:"confirm"`
	}
	readJSON(r, &body)
	if body.Confirm != s.Host.Hostname {
		writeErrorMsg(w, http.StatusBadRequest, "confirm", "type the server name to confirm")
		return
	}
	s.audit(r, "system.reboot", s.Host.Hostname, nil)
	writeJSON(w, http.StatusAccepted, map[string]bool{"rebooting": true})
	go func() {
		time.Sleep(2 * time.Second)
		ctx := context.WithoutCancel(r.Context())
		if _, err := s.Runner.Sudo(ctx, 30*time.Second, "systemctl", "reboot"); err != nil {
			s.Store.Audit(ctx, auditEntry("", "system.reboot", s.Host.Hostname, err))
		}
	}()
}
