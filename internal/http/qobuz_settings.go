package httpapp

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cesargomez89/navidrums/internal/catalog"
	"github.com/cesargomez89/navidrums/internal/store"
)

// qobuzSettingKeys maps the JSON field the UI posts onto its settings key.
var qobuzSettingKeys = map[string]string{
	"app_id":       store.SettingQobuzAppID,
	"app_secret":   store.SettingQobuzAppSecret,
	"auth_token":   store.SettingQobuzAuthToken,
	"email":        store.SettingQobuzEmail,
	"password_md5": store.SettingQobuzPasswordMD5,
}

// maskSecret shows just enough of a stored secret to recognise it without
// handing the whole value back to the browser.
func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("•", len(value))
	}
	return strings.Repeat("•", 8) + value[len(value)-4:]
}

// GetQobuzCredentialsHTMX reports which credentials are configured, masked, and
// whether each came from Settings or the environment.
func (h *Handler) GetQobuzCredentialsHTMX(w http.ResponseWriter, r *http.Request) {
	effective := h.ProviderManager.QobuzCredentials()

	fromSettings := func(key string) bool {
		val, err := h.SettingsRepo.Get(key)
		return err == nil && val != ""
	}

	response := map[string]interface{}{
		"app_id":     effective.AppID, // not secret: it identifies the app, not the user
		"app_secret": maskSecret(effective.AppSecret),
		"auth_token": maskSecret(effective.AuthToken),
		"email":      effective.Email,
		"password":   maskSecret(effective.PasswordMD5),
		"sources": map[string]bool{
			"app_id":     fromSettings(store.SettingQobuzAppID),
			"app_secret": fromSettings(store.SettingQobuzAppSecret),
			"auth_token": fromSettings(store.SettingQobuzAuthToken),
			"email":      fromSettings(store.SettingQobuzEmail),
			"password":   fromSettings(store.SettingQobuzPasswordMD5),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.Logger.Error("Failed to encode qobuz credentials", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// SetQobuzCredentialsHTMX saves credentials entered in Settings. Only fields
// present in the request are touched, and an empty value clears the stored
// setting so the environment value applies again.
func (h *Handler) SetQobuzCredentialsHTMX(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	for field, value := range body {
		key, ok := qobuzSettingKeys[field]
		if !ok {
			// A plaintext password is hashed here so it is never stored as-is.
			if field == "password" {
				key = store.SettingQobuzPasswordMD5
				value = catalog.QobuzPasswordHash(value)
			} else {
				http.Error(w, "Unknown field: "+field, http.StatusBadRequest)
				return
			}
		}

		value = strings.TrimSpace(value)

		var err error
		if value == "" {
			err = h.SettingsRepo.Delete(key)
		} else {
			err = h.SettingsRepo.Set(key, value)
		}
		if err != nil {
			h.Logger.Error("Failed to save qobuz credential", "key", key, "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
	}

	h.ProviderManager.InvalidateAllCaches()
	_, _ = w.Write([]byte(`{"success":true}`))
}

// GetQobuzStatusHTMX probes Qobuz and reports which credentials still work, so
// an expired token or a rotated app secret is visible before a download fails.
func (h *Handler) GetQobuzStatusHTMX(w http.ResponseWriter, r *http.Request) {
	status := h.ProviderManager.CheckQobuzCredentials(r.Context())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		h.Logger.Error("Failed to encode qobuz status", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
