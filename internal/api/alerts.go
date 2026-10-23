package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/beedywool/vps-admin/internal/alerts"
)

// Alerts: the webhook that receives what breaks on the server. The watching itself is
// in internal/alerts; here are only the settings and the test button.

func (s *Server) alertsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.alertsView(r))
}

func (s *Server) alertsView(r *http.Request) map[string]any {
	cfg := alerts.ReadConfig(r.Context(), s.Store)
	view := map[string]any{"enabled": cfg.Enabled, "webhook": cfg.Webhook, "disk": cfg.Disk}
	if s.Alerts != nil {
		msg, at := s.Alerts.LastError()
		view["error"] = msg
		if !at.IsZero() {
			view["errorAt"] = at.Unix()
		}
	}
	return view
}

func (s *Server) alertsPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled *bool   `json:"enabled"`
		Webhook *string `json:"webhook"`
		Disk    *int    `json:"disk"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx := r.Context()
	hook := ""
	if body.Webhook != nil {
		hook = strings.TrimSpace(*body.Webhook)
		if hook != "" && !alerts.ValidWebhook(hook) {
			writeErrorMsg(w, http.StatusBadRequest, "invalid", "use an http or https URL")
			return
		}
	}
	if body.Disk != nil && (*body.Disk < alerts.DiskMin || *body.Disk > alerts.DiskMax) {
		writeErrorMsg(w, http.StatusBadRequest, "invalid",
			"the disk threshold is a percentage between "+strconv.Itoa(alerts.DiskMin)+" and "+strconv.Itoa(alerts.DiskMax))
		return
	}
	var err error
	if body.Enabled != nil {
		err = s.Store.SetSetting(ctx, alerts.SettingEnabled, strconv.FormatBool(*body.Enabled))
	}
	if err == nil && body.Webhook != nil {
		err = s.Store.SetSetting(ctx, alerts.SettingWebhook, hook)
	}
	if err == nil && body.Disk != nil {
		err = s.Store.SetSetting(ctx, alerts.SettingDisk, strconv.Itoa(*body.Disk))
	}
	s.audit(r, "settings.alerts", "", err)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	// A new URL starts clean: the old failure belonged to the old address.
	if body.Webhook != nil && s.Alerts != nil {
		s.Alerts.ClearError()
	}
	writeJSON(w, http.StatusOK, s.alertsView(r))
}

// alertsTest posts a sample to the stored webhook, or to the one being typed, so the
// settings page can prove the URL before anyone relies on it.
func (s *Server) alertsTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Webhook string `json:"webhook"`
	}
	readJSON(r, &body) // an empty body tests the saved one
	hook := strings.TrimSpace(body.Webhook)
	if hook == "" {
		hook = alerts.ReadConfig(r.Context(), s.Store).Webhook
	}
	if hook == "" {
		writeErrorMsg(w, http.StatusBadRequest, "no_webhook", "give a webhook URL first")
		return
	}
	if !alerts.ValidWebhook(hook) {
		writeErrorMsg(w, http.StatusBadRequest, "invalid", "use an http or https URL")
		return
	}
	if err := alerts.Test(r.Context(), nil, hook); err != nil {
		writeErrorMsg(w, http.StatusBadGateway, "webhook_failed", err.Error())
		return
	}
	if s.Alerts != nil {
		s.Alerts.ClearError()
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}
