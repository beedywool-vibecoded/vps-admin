package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/beedywool/vps-admin/internal/assistant"
	"github.com/beedywool/vps-admin/internal/auth"
	"github.com/beedywool/vps-admin/internal/store"
)

func (s *Server) assistantSettings(w http.ResponseWriter, r *http.Request) {
	st := s.Assistant.Settings(r.Context())
	models := []assistant.Model{}
	for _, p := range st.Providers {
		if p.ID == st.Provider {
			models = p.Models
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": st, "models": models})
}

func (s *Server) assistantUpdate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider  *string `json:"provider"`
		APIKey    *string `json:"apiKey"`
		Model     string  `json:"model"`
		Effort    string  `json:"effort"`
		AutoRead  *bool   `json:"autoRead"`
		OllamaURL *string `json:"ollamaUrl"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx := r.Context()
	if body.Provider != nil {
		if err := s.Assistant.SetProvider(ctx, *body.Provider); err != nil {
			writeErrorMsg(w, http.StatusBadRequest, "invalid", err.Error())
			return
		}
	}
	// The key always belongs to the provider that is active after the switch above, so
	// the panel can send "switch to Gemini and use this key" in one request.
	if body.APIKey != nil {
		provider := s.Assistant.ProviderID(ctx)
		err := s.Assistant.SetKey(ctx, provider, *body.APIKey)
		action := "assistant.key_set"
		if *body.APIKey == "" {
			action = "assistant.key_removed"
		}
		s.audit(r, action, provider, err)
		if err != nil {
			writeErrorMsg(w, http.StatusBadRequest, "invalid_key", err.Error())
			return
		}
	}
	if body.OllamaURL != nil {
		if err := s.Assistant.SetOllamaURL(ctx, *body.OllamaURL); err != nil {
			writeErrorMsg(w, http.StatusBadRequest, "invalid_url", err.Error())
			return
		}
	}
	if err := s.Assistant.SetOptions(ctx, body.Model, body.Effort, body.AutoRead); err != nil {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	s.assistantSettings(w, r)
}

func (s *Server) conversations(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Conversations(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

func assistantStatus(err error) int {
	switch {
	case errors.Is(err, assistant.ErrNoKey):
		return http.StatusPreconditionFailed
	case errors.Is(err, assistant.ErrRunning):
		return http.StatusConflict
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func (s *Server) conversationCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string `json:"message"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	c, err := s.Assistant.NewConversation(r.Context(), body.Message, auth.FromContext(r.Context()).DeviceID)
	if err != nil {
		writeErrorMsg(w, assistantStatus(err), "assistant_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"conversation": c})
}

func (s *Server) conversationGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.Store.Conversation(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	msgs, _ := s.Store.Messages(r.Context(), id)
	type msgView struct {
		Seq     int64           `json:"seq"`
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	views := make([]msgView, 0, len(msgs))
	for _, m := range msgs {
		views = append(views, msgView{m.Seq, m.Role, json.RawMessage(m.Content)})
	}
	calls, _ := s.Store.ToolCalls(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"conversation": c, "messages": views, "toolCalls": calls, "running": s.Assistant.Running(id)})
}

func (s *Server) conversationSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string `json:"message"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	id := r.PathValue("id")
	if _, err := s.Store.Conversation(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err := s.Assistant.Send(r.Context(), id, body.Message, auth.FromContext(r.Context()).DeviceID); err != nil {
		writeErrorMsg(w, assistantStatus(err), "assistant_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) conversationStop(w http.ResponseWriter, r *http.Request) {
	s.Assistant.Stop(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) conversationDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.Assistant.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeErrorMsg(w, assistantStatus(err), "delete_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// toolDecide approves or denies an action the assistant asked for. Every decision is
// in the audit log with the device that made it.
func (s *Server) toolDecide(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Approve bool `json:"approve"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	tc, err := s.Assistant.Decide(r.Context(), r.PathValue("call"), body.Approve, auth.FromContext(r.Context()).DeviceID)
	action := "assistant.deny"
	if body.Approve {
		action = "assistant.approve"
	}
	target := r.PathValue("call")
	if tc != nil {
		target = tc.Summary
		if tc.IsError && body.Approve {
			err = errors.New(firstLine(tc.Result))
		}
	}
	s.audit(r, action, target, err)
	if err != nil && tc == nil {
		writeErrorMsg(w, http.StatusConflict, "decide_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tc)
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}

// conversationStream pushes text, thinking, tool and state events while the
// conversation is open in the panel.
func (s *Server) conversationStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Store.Conversation(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Assistant.Subscribe(id)
	defer cancel()
	fmt.Fprint(w, ": open\n\n")
	rc.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			b, _ := json.Marshal(ev.Data)
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b); err != nil || rc.Flush() != nil {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
