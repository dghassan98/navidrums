package httpapp

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cesargomez89/navidrums/internal/notify"
	"github.com/cesargomez89/navidrums/internal/store"
)

// notifyWebhookURL resolves the webhook the same way the worker does: a value
// saved in Settings wins over NOTIFY_URL.
func (h *Handler) notifyWebhookURL() string {
	if h.SettingsRepo != nil {
		if stored, err := h.SettingsRepo.Get(store.SettingNotifyURL); err == nil && stored != "" {
			return stored
		}
	}
	if h.Config != nil {
		return h.Config.NotifyURL
	}
	return ""
}

// maskWebhookURL keeps the host and shape visible while hiding the token, which
// is the part that lets anyone post to the channel.
func maskWebhookURL(raw string) string {
	if raw == "" {
		return ""
	}

	trimmed := strings.TrimRight(raw, "/")
	slash := strings.LastIndex(trimmed, "/")
	if slash < 0 || slash == len(trimmed)-1 {
		return maskSecret(trimmed)
	}

	return trimmed[:slash+1] + maskSecret(trimmed[slash+1:])
}

// GetNotificationsHTMX reports the configured webhook, masked.
func (h *Handler) GetNotificationsHTMX(w http.ResponseWriter, r *http.Request) {
	current := h.notifyWebhookURL()

	fromSettings := false
	if h.SettingsRepo != nil {
		stored, err := h.SettingsRepo.Get(store.SettingNotifyURL)
		fromSettings = err == nil && stored != ""
	}

	response := map[string]interface{}{
		"configured":    current != "",
		"masked":        maskWebhookURL(current),
		"from_settings": fromSettings,
		"kind":          webhookKind(current),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.Logger.Error("Failed to encode notification settings", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func webhookKind(raw string) string {
	switch {
	case raw == "":
		return "none"
	case notify.IsDiscordWebhook(raw):
		return "discord"
	default:
		return "apprise"
	}
}

// SetNotificationsHTMX saves the webhook. An empty value clears it, falling
// back to NOTIFY_URL.
func (h *Handler) SetNotificationsHTMX(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	value := strings.TrimSpace(body.URL)
	if value != "" && !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
		http.Error(w, "Webhook must be an http(s) URL", http.StatusBadRequest)
		return
	}

	var err error
	if value == "" {
		err = h.SettingsRepo.Delete(store.SettingNotifyURL)
	} else {
		err = h.SettingsRepo.Set(store.SettingNotifyURL, value)
	}
	if err != nil {
		h.Logger.Error("Failed to save webhook", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write([]byte(`{"success":true}`))
}

// TestNotificationHTMX sends a sample notification so the webhook can be
// verified without waiting for a download to finish.
func (h *Handler) TestNotificationHTMX(w http.ResponseWriter, r *http.Request) {
	webhook := h.notifyWebhookURL()
	if webhook == "" {
		http.Error(w, "No webhook configured", http.StatusBadRequest)
		return
	}

	notifier := notify.New(func() string { return webhook }, h.Logger)
	event := notify.Event{
		Status:  notify.StatusCompleted,
		JobType: "track",
		Title:   "Test notification",
		Artist:  "Navidrums",
		Album:   "If you can read this, notifications work",
	}

	if err := notifier.Send(r.Context(), webhook, event); err != nil {
		h.Logger.Error("Test notification failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_, _ = w.Write([]byte(`{"success":true}`))
}
