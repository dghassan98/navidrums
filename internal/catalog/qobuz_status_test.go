package catalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// qobuzStatusServer answers the two probe endpoints with the given statuses.
func qobuzStatusServer(t *testing.T, accountStatus, signatureStatus int) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, qobuzPaidLogin)
	})
	mux.HandleFunc("/favorite/getUserFavorites", func(w http.ResponseWriter, r *http.Request) {
		if accountStatus != http.StatusOK {
			w.WriteHeader(accountStatus)
			return
		}
		_, _ = io.WriteString(w, `{"tracks":{"items":[]}}`)
	})
	mux.HandleFunc("/track/getFileUrl", func(w http.ResponseWriter, r *http.Request) {
		if signatureStatus != http.StatusOK {
			w.WriteHeader(signatureStatus)
			return
		}
		fmt.Fprint(w, `{"track_id":19512574,"url":"https://example.test/a.flac"}`)
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestQobuzCheckCredentials(t *testing.T) {
	full := testQobuzCredentials()

	tests := []struct {
		name            string
		creds           QobuzCredentials
		accountStatus   int
		signatureStatus int
		wantAppID       QobuzCredentialState
		wantAccount     QobuzCredentialState
		wantSecret      QobuzCredentialState
		wantBrowse      bool
		wantDownload    bool
	}{
		{
			name:            "everything works",
			creds:           full,
			accountStatus:   http.StatusOK,
			signatureStatus: http.StatusOK,
			wantAppID:       QobuzStateOK,
			wantAccount:     QobuzStateOK,
			wantSecret:      QobuzStateOK,
			wantBrowse:      true,
			wantDownload:    true,
		},
		{
			name:            "rotated app secret breaks downloads only",
			creds:           full,
			accountStatus:   http.StatusOK,
			signatureStatus: http.StatusBadRequest,
			wantAppID:       QobuzStateOK,
			wantAccount:     QobuzStateOK,
			wantSecret:      QobuzStateRejected,
			wantBrowse:      true,
			wantDownload:    false,
		},
		{
			name:            "expired token is caught before the signature probe",
			creds:           QobuzCredentials{AppID: full.AppID, AppSecret: full.AppSecret, AuthToken: "stale"},
			accountStatus:   http.StatusUnauthorized,
			signatureStatus: http.StatusOK,
			wantAppID:       QobuzStateOK,
			wantAccount:     QobuzStateRejected,
			wantSecret:      QobuzStateUnchecked,
			wantBrowse:      false,
			wantDownload:    false,
		},
		{
			name:            "no secret still allows browsing",
			creds:           QobuzCredentials{AppID: full.AppID, AuthToken: "tok"},
			accountStatus:   http.StatusOK,
			signatureStatus: http.StatusOK,
			wantAppID:       QobuzStateOK,
			wantAccount:     QobuzStateOK,
			wantSecret:      QobuzStateMissing,
			wantBrowse:      true,
			wantDownload:    false,
		},
		{
			name:            "missing app id fails immediately",
			creds:           QobuzCredentials{AuthToken: "tok"},
			accountStatus:   http.StatusOK,
			signatureStatus: http.StatusOK,
			wantAppID:       QobuzStateMissing,
			wantAccount:     QobuzStateMissing,
			wantSecret:      QobuzStateUnchecked,
			wantBrowse:      false,
			wantDownload:    false,
		},
		{
			name:            "no account credentials at all",
			creds:           QobuzCredentials{AppID: full.AppID, AppSecret: full.AppSecret},
			accountStatus:   http.StatusOK,
			signatureStatus: http.StatusOK,
			wantAppID:       QobuzStateOK,
			wantAccount:     QobuzStateMissing,
			wantSecret:      QobuzStateUnchecked,
			wantBrowse:      false,
			wantDownload:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := qobuzStatusServer(t, tt.accountStatus, tt.signatureStatus)
			provider := NewQobuzDirectProvider(server.URL, tt.creds)

			status := provider.CheckCredentials(context.Background())

			if status.AppID != tt.wantAppID {
				t.Errorf("AppID = %q, want %q", status.AppID, tt.wantAppID)
			}
			if status.Account != tt.wantAccount {
				t.Errorf("Account = %q, want %q", status.Account, tt.wantAccount)
			}
			if status.AppSecret != tt.wantSecret {
				t.Errorf("AppSecret = %q, want %q", status.AppSecret, tt.wantSecret)
			}
			if status.CanBrowse != tt.wantBrowse {
				t.Errorf("CanBrowse = %v, want %v", status.CanBrowse, tt.wantBrowse)
			}
			if status.CanDownload != tt.wantDownload {
				t.Errorf("CanDownload = %v, want %v", status.CanDownload, tt.wantDownload)
			}
			if status.Message == "" {
				t.Error("Message is empty; the UI relies on it to explain the state")
			}
		})
	}
}
