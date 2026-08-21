package httpapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// adminCookieName holds the proof that the settings password was entered.
const adminCookieName = "navidrums_admin"

// adminSessionTTL is how long an unlocked settings session lasts.
const adminSessionTTL = 12 * time.Hour

// AdminLocked reports whether settings are password protected. With no admin
// password configured the gate is disabled entirely, which keeps existing
// installs working as before.
func (h *Handler) AdminLocked() bool {
	return h.Config != nil && h.Config.AdminPassword != ""
}

// adminToken derives the cookie value for an expiry. It is an HMAC keyed by the
// admin password, so the cookie cannot be forged without knowing the password
// and every cookie is invalidated the moment the password changes.
func (h *Handler) adminToken(expiry int64) string {
	mac := hmac.New(sha256.New, []byte(h.Config.AdminPassword))
	fmt.Fprintf(mac, "navidrums-admin:%d", expiry)
	return strconv.FormatInt(expiry, 10) + ":" + hex.EncodeToString(mac.Sum(nil))
}

// adminUnlocked reports whether the request carries a valid, unexpired session.
func (h *Handler) adminUnlocked(r *http.Request) bool {
	if !h.AdminLocked() {
		return true
	}

	cookie, err := r.Cookie(adminCookieName)
	if err != nil {
		return false
	}

	rawExpiry, _, found := strings.Cut(cookie.Value, ":")
	if !found {
		return false
	}
	expiry, err := strconv.ParseInt(rawExpiry, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return false
	}

	return hmac.Equal([]byte(cookie.Value), []byte(h.adminToken(expiry)))
}

// requireAdmin blocks settings routes until the password has been entered.
func (h *Handler) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.adminUnlocked(r) {
			next.ServeHTTP(w, r)
			return
		}

		// The settings page renders its own unlock form; everything else is an
		// API call and gets a plain refusal.
		if r.Method == http.MethodGet && r.URL.Path == "/settings" {
			h.RenderPage(w, "settings_locked.html", map[string]interface{}{
				"ActivePage": "settings",
			})
			return
		}

		http.Error(w, "Settings are locked. Unlock them on the Settings page.", http.StatusForbidden)
	})
}

// AdminUnlockHTMX checks the submitted password and starts a session.
func (h *Handler) AdminUnlockHTMX(w http.ResponseWriter, r *http.Request) {
	if !h.AdminLocked() {
		http.Error(w, "Settings are not password protected", http.StatusBadRequest)
		return
	}

	password := r.FormValue("password")
	if subtleEqual(password, h.Config.AdminPassword) {
		expiry := time.Now().Add(adminSessionTTL).Unix()
		http.SetCookie(w, &http.Cookie{
			Name:     adminCookieName,
			Value:    h.adminToken(expiry),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Unix(expiry, 0),
		})
		w.Header().Set("HX-Redirect", "/settings")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Slow down guessing a little without blocking the server.
	time.Sleep(500 * time.Millisecond)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`<p class="text-danger">Incorrect password.</p>`))
}

// AdminLockHTMX ends the settings session.
func (h *Handler) AdminLockHTMX(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	w.Header().Set("HX-Redirect", "/settings")
	w.WriteHeader(http.StatusNoContent)
}

// subtleEqual compares two secrets in constant time.
func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
