package httpapp

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/cesargomez89/navidrums/internal/config"
)

func testAdminHandler(password string) *Handler {
	return &Handler{Config: &config.Config{AdminPassword: password}}
}

func TestAdminLocked(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     bool
	}{
		{"password set locks settings", "s3cret", true},
		{"empty password leaves settings open", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := testAdminHandler(tt.password).AdminLocked(); got != tt.want {
				t.Errorf("AdminLocked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAdminUnlocked(t *testing.T) {
	h := testAdminHandler("s3cret")
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	// A cookie minted by a handler with a different password must not pass.
	other := testAdminHandler("different")

	tests := []struct {
		name   string
		cookie string
		want   bool
	}{
		{"valid session", h.adminToken(future), true},
		{"expired session", h.adminToken(past), false},
		{"cookie from another password", other.adminToken(future), false},
		{"forged mac", strconv.FormatInt(future, 10) + ":deadbeef", false},
		{"malformed cookie", "nonsense", false},
		{"empty cookie", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/settings", nil)
			if tt.cookie != "" {
				r.AddCookie(&http.Cookie{Name: adminCookieName, Value: tt.cookie})
			}
			if got := h.adminUnlocked(r); got != tt.want {
				t.Errorf("adminUnlocked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAdminUnlockedWithoutPassword(t *testing.T) {
	// With no admin password configured every request is allowed through, so
	// existing installs keep working.
	h := testAdminHandler("")
	r := httptest.NewRequest(http.MethodGet, "/settings", nil)

	if !h.adminUnlocked(r) {
		t.Error("adminUnlocked() = false, want true when no password is configured")
	}
}

func TestRequireAdminBlocksAPIRoutes(t *testing.T) {
	h := testAdminHandler("s3cret")
	guarded := h.requireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name     string
		path     string
		cookie   string
		wantHTTP int
	}{
		{"locked api call is refused", "/htmx/providers", "", http.StatusForbidden},
		{"unlocked api call passes", "/htmx/providers", h.adminToken(time.Now().Add(time.Hour).Unix()), http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.cookie != "" {
				r.AddCookie(&http.Cookie{Name: adminCookieName, Value: tt.cookie})
			}
			w := httptest.NewRecorder()
			guarded.ServeHTTP(w, r)

			if w.Code != tt.wantHTTP {
				t.Errorf("status = %d, want %d", w.Code, tt.wantHTTP)
			}
		})
	}
}

func TestMaskSecret(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"long secret keeps last four", "0123456789abcdef", "••••••••cdef"},
		{"short secret fully masked", "abc", "•••"},
		{"exactly four fully masked", "abcd", "••••"},
		{"empty stays empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := maskSecret(tt.value); got != tt.want {
				t.Errorf("maskSecret(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
