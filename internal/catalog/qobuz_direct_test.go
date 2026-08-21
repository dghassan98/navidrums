package catalog

import (
	"context"
	"crypto/md5" //nolint:gosec // mirrors the signature the Qobuz API requires
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cesargomez89/navidrums/internal/constants"
)

const (
	testQobuzAppID     = "123456789"
	testQobuzAppSecret = "0123456789abcdef0123456789abcdef"
	testQobuzToken     = "test-auth-token"
)

func testQobuzCredentials() QobuzCredentials {
	return QobuzCredentials{
		AppID:       testQobuzAppID,
		AppSecret:   testQobuzAppSecret,
		Email:       "listener@example.test",
		PasswordMD5: QobuzPasswordHash("hunter2"),
	}
}

func TestQobuzDirectFormatID(t *testing.T) {
	tests := []struct {
		name    string
		quality string
		want    int
	}{
		{"hi res lossless", constants.QualityHiResLossless, 27},
		{"lossless", constants.QualityLossless, 6},
		{"high maps to mp3 320", constants.QualityHigh, 5},
		{"low maps to mp3 320", constants.QualityLow, 5},
		{"unknown falls back to lossless", "WHATEVER", 6},
		{"empty falls back to lossless", "", 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qobuzDirectFormatID(tt.quality); got != tt.want {
				t.Errorf("qobuzDirectFormatID(%q) = %d, want %d", tt.quality, got, tt.want)
			}
		})
	}
}

func TestQobuzPasswordHash(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     string
	}{
		{"known digest", "hunter2", "2ab96390c7dbe3439de74d0c9b0b1767"},
		{"empty digest", "", "d41d8cd98f00b204e9800998ecf8427e"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := QobuzPasswordHash(tt.password); got != tt.want {
				t.Errorf("QobuzPasswordHash(%q) = %q, want %q", tt.password, got, tt.want)
			}
		})
	}
}

func TestQobuzRequestSignature(t *testing.T) {
	// The signature is md5 over the endpoint, then each parameter name and
	// value in alphabetical order, then the timestamp and the app secret.
	raw := "trackgetFileUrl" + "format_id6" + "intentstream" + "track_id12345" + "1670000000" + testQobuzAppSecret
	sum := md5.Sum([]byte(raw)) //nolint:gosec // mirrors the Qobuz API
	want := hex.EncodeToString(sum[:])

	got := qobuzRequestSignature(6, "12345", "1670000000", testQobuzAppSecret)
	if got != want {
		t.Errorf("qobuzRequestSignature() = %q, want %q", got, want)
	}
}

func TestQobuzCredentialsCanAuthenticate(t *testing.T) {
	full := testQobuzCredentials()

	tests := []struct {
		name  string
		creds QobuzCredentials
		want  bool
	}{
		{"email and password", full, true},
		{"auth token instead of password", QobuzCredentials{AppID: full.AppID, AuthToken: "tok"}, true},
		{"missing app id", QobuzCredentials{Email: full.Email, PasswordMD5: full.PasswordMD5}, false},
		{"secret is not needed to authenticate", QobuzCredentials{AppID: full.AppID, Email: full.Email, PasswordMD5: full.PasswordMD5}, true},
		{"missing email", QobuzCredentials{AppID: full.AppID, AppSecret: full.AppSecret, PasswordMD5: full.PasswordMD5}, false},
		{"missing password", QobuzCredentials{AppID: full.AppID, AppSecret: full.AppSecret, Email: full.Email}, false},
		{"empty", QobuzCredentials{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.creds.CanAuthenticate(); got != tt.want {
				t.Errorf("CanAuthenticate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewQobuzDirectProviderBaseURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty uses official api", "", constants.QobuzDirectDefaultURL},
		{"blank uses official api", "  ", constants.QobuzDirectDefaultURL},
		{"trailing slash trimmed", "https://example.test/api/", "https://example.test/api"},
		{"custom kept", "https://example.test/api", "https://example.test/api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewQobuzDirectProvider(tt.input, QobuzCredentials{}).BaseURL
			if got != tt.want {
				t.Errorf("NewQobuzDirectProvider(%q).BaseURL = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// qobuzDirectServer stands in for the official Qobuz API. loginBody controls
// what user/login answers; fileURLBody controls track/getFileUrl.
type qobuzDirectServer struct {
	*httptest.Server

	mu           sync.Mutex
	loginCalls   int
	fileURLQuery map[string]string
	tokensSeen   []string
}

func newQobuzDirectServer(t *testing.T, loginBody string, fileURL func(serverURL string) string) *qobuzDirectServer {
	t.Helper()

	qs := &qobuzDirectServer{fileURLQuery: map[string]string{}}
	mux := http.NewServeMux()

	mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
		qs.mu.Lock()
		qs.loginCalls++
		qs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, loginBody)
	})

	mux.HandleFunc("/track/getFileUrl", func(w http.ResponseWriter, r *http.Request) {
		qs.mu.Lock()
		for key := range r.URL.Query() {
			qs.fileURLQuery[key] = r.URL.Query().Get(key)
		}
		qs.tokensSeen = append(qs.tokensSeen, r.Header.Get("X-User-Auth-Token"))
		qs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fileURL(qs.Server.URL))
	})

	mux.HandleFunc("/audio.flac", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", constants.MimeTypeFLAC)
		_, _ = io.WriteString(w, testAudioBody)
	})

	qs.Server = httptest.NewServer(mux)
	t.Cleanup(qs.Server.Close)

	return qs
}

const qobuzPaidLogin = `{"user_auth_token":"test-auth-token","user":{"id":1,` +
	`"credential":{"parameters":{"label":"Studio","lossless_streaming":true}}}}`

const qobuzFreeLogin = `{"user_auth_token":"test-auth-token","user":{"id":1,` +
	`"credential":{"parameters":null}}}`

func TestQobuzDirectGetStream(t *testing.T) {
	server := newQobuzDirectServer(t, qobuzPaidLogin, func(serverURL string) string {
		return fmt.Sprintf(`{"track_id":12345,"format_id":6,"mime_type":"audio/flac","url":%q}`,
			serverURL+"/audio.flac")
	})

	provider := NewQobuzDirectProvider(server.Server.URL, testQobuzCredentials())
	stream, mimeType, err := provider.GetStream(context.Background(), "12345", "", constants.QualityLossless)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}

	if got := readStream(t, stream); got != testAudioBody {
		t.Errorf("stream body = %q, want %q", got, testAudioBody)
	}
	if mimeType != constants.MimeTypeFLAC {
		t.Errorf("mimeType = %q, want %q", mimeType, constants.MimeTypeFLAC)
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	if server.loginCalls != 1 {
		t.Errorf("login calls = %d, want 1", server.loginCalls)
	}
	if got := server.fileURLQuery["format_id"]; got != "6" {
		t.Errorf("format_id = %q, want 6", got)
	}
	if got := server.fileURLQuery["intent"]; got != "stream" {
		t.Errorf("intent = %q, want stream", got)
	}
	if got := server.fileURLQuery["app_id"]; got != testQobuzAppID {
		t.Errorf("app_id = %q, want %q", got, testQobuzAppID)
	}

	wantSig := qobuzRequestSignature(6, "12345", server.fileURLQuery["request_ts"], testQobuzAppSecret)
	if got := server.fileURLQuery["request_sig"]; got != wantSig {
		t.Errorf("request_sig = %q, want %q", got, wantSig)
	}
	if len(server.tokensSeen) == 0 || server.tokensSeen[0] != testQobuzToken {
		t.Errorf("auth token sent = %v, want %q", server.tokensSeen, testQobuzToken)
	}
}

func TestQobuzDirectGetStreamReportsRestriction(t *testing.T) {
	server := newQobuzDirectServer(t, qobuzPaidLogin, func(string) string {
		return `{"track_id":12345,"url":"","restrictions":[{"code":"TrackRestrictedByRights"}]}`
	})

	provider := NewQobuzDirectProvider(server.Server.URL, testQobuzCredentials())
	stream, _, err := provider.GetStream(context.Background(), "12345", "", constants.QualityLossless)
	if err == nil {
		_ = stream.Close()
		t.Fatal("GetStream succeeded on a restricted track")
	}
	if !errors.Is(err, ErrQobuzNotStreamable) {
		t.Errorf("error = %v, want ErrQobuzNotStreamable", err)
	}
	if !strings.Contains(err.Error(), "TrackRestrictedByRights") {
		t.Errorf("error %v does not report the restriction code", err)
	}
}

func TestQobuzDirectRejectsFreeAccount(t *testing.T) {
	server := newQobuzDirectServer(t, qobuzFreeLogin, func(string) string { return `{}` })

	provider := NewQobuzDirectProvider(server.Server.URL, testQobuzCredentials())
	stream, _, err := provider.GetStream(context.Background(), "12345", "", constants.QualityLossless)
	if err == nil {
		_ = stream.Close()
		t.Fatal("GetStream succeeded on a free account")
	}
	if !errors.Is(err, ErrQobuzIneligible) {
		t.Errorf("error = %v, want ErrQobuzIneligible", err)
	}
}

func TestQobuzDirectRequiresCredentials(t *testing.T) {
	provider := NewQobuzDirectProvider("https://example.test", QobuzCredentials{})

	stream, _, err := provider.GetStream(context.Background(), "12345", "", constants.QualityLossless)
	if err == nil {
		_ = stream.Close()
		t.Fatal("GetStream succeeded without credentials")
	}
	if !errors.Is(err, ErrQobuzCredentialsMissing) {
		t.Errorf("error = %v, want ErrQobuzCredentialsMissing", err)
	}
}

func TestQobuzDirectResolveTrackID(t *testing.T) {
	tests := []struct {
		name   string
		isrc   string
		search string
		want   string
	}{
		{
			name:   "matching isrc wins",
			isrc:   "GBDUW0000053",
			search: `{"tracks":{"items":[{"id":999,"isrc":"OTHER0000001"},{"id":12345,"isrc":"GBDUW0000053"}]}}`,
			want:   "12345",
		},
		{
			name:   "mismatched isrc is not trusted",
			isrc:   "GBDUW0000053",
			search: `{"tracks":{"items":[{"id":999,"isrc":"OTHER0000001"}]}}`,
			want:   "fallback-id",
		},
		{
			name:   "no results keeps the given id",
			isrc:   "GBDUW0000053",
			search: `{"tracks":{"items":[]}}`,
			want:   "fallback-id",
		},
		{
			name: "no isrc keeps the given id",
			isrc: "",
			want: "fallback-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, qobuzPaidLogin)
			})
			mux.HandleFunc("/track/search", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.search)
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			provider := NewQobuzDirectProvider(server.URL, testQobuzCredentials())
			got := provider.resolveTrackID(context.Background(), "fallback-id", tt.isrc)
			if got != tt.want {
				t.Errorf("resolveTrackID(%q) = %q, want %q", tt.isrc, got, tt.want)
			}
		})
	}
}

func TestQobuzDirectReLoginsOnUnauthorized(t *testing.T) {
	var mu sync.Mutex
	var fileURLCalls int
	logins := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		logins++
		mu.Unlock()
		_, _ = io.WriteString(w, qobuzPaidLogin)
	})

	var server *httptest.Server
	mux.HandleFunc("/track/getFileUrl", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fileURLCalls++
		first := fileURLCalls == 1
		mu.Unlock()

		// Qobuz rejects a stale token once; the retry must succeed.
		if first {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprintf(w, `{"track_id":12345,"mime_type":"audio/flac","url":%q}`, server.URL+"/audio.flac")
	})
	mux.HandleFunc("/audio.flac", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, testAudioBody)
	})

	server = httptest.NewServer(mux)
	defer server.Close()

	provider := NewQobuzDirectProvider(server.URL, testQobuzCredentials())
	stream, _, err := provider.GetStream(context.Background(), "12345", "", constants.QualityLossless)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}
	if got := readStream(t, stream); got != testAudioBody {
		t.Errorf("stream body = %q, want %q", got, testAudioBody)
	}

	mu.Lock()
	defer mu.Unlock()
	if logins != 2 {
		t.Errorf("login calls = %d, want 2 (initial plus refresh)", logins)
	}
	if fileURLCalls != 2 {
		t.Errorf("getFileUrl calls = %d, want 2", fileURLCalls)
	}
}

func TestNewProviderWithCredentialsReturnsQobuzDirect(t *testing.T) {
	provider := NewProviderWithCredentials(ProviderTypeQobuzDirect, "", testQobuzCredentials())
	direct, ok := provider.(*QobuzDirectProvider)
	if !ok {
		t.Fatalf("NewProviderWithCredentials(qobuz-direct) = %T, want *QobuzDirectProvider", provider)
	}
	if direct.creds.AppID != testQobuzAppID {
		t.Errorf("credentials were not passed through: app id = %q", direct.creds.AppID)
	}
	if direct.BaseURL != constants.QobuzDirectDefaultURL {
		t.Errorf("BaseURL = %q, want %q", direct.BaseURL, constants.QobuzDirectDefaultURL)
	}
}

func TestQobuzDirectUsesAuthTokenWithoutLogin(t *testing.T) {
	var mu sync.Mutex
	logins := 0
	var tokenSeen string

	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		logins++
		mu.Unlock()
		_, _ = io.WriteString(w, qobuzPaidLogin)
	})
	mux.HandleFunc("/track/getFileUrl", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokenSeen = r.Header.Get("X-User-Auth-Token")
		mu.Unlock()
		fmt.Fprintf(w, `{"track_id":12345,"mime_type":"audio/flac","url":%q}`, server.URL+"/audio.flac")
	})
	mux.HandleFunc("/audio.flac", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, testAudioBody)
	})
	server = httptest.NewServer(mux)
	defer server.Close()

	creds := QobuzCredentials{
		AppID:     testQobuzAppID,
		AppSecret: testQobuzAppSecret,
		AuthToken: "supplied-token",
	}

	provider := NewQobuzDirectProvider(server.URL, creds)
	stream, _, err := provider.GetStream(context.Background(), "12345", "", constants.QualityLossless)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}
	if got := readStream(t, stream); got != testAudioBody {
		t.Errorf("stream body = %q, want %q", got, testAudioBody)
	}

	mu.Lock()
	defer mu.Unlock()
	if logins != 0 {
		t.Errorf("login calls = %d, want 0 (a supplied token skips login entirely)", logins)
	}
	if tokenSeen != "supplied-token" {
		t.Errorf("token sent = %q, want %q", tokenSeen, "supplied-token")
	}
}

func TestQobuzDirectRejectedTokenCannotRefresh(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/track/getFileUrl", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	creds := QobuzCredentials{
		AppID:     testQobuzAppID,
		AppSecret: testQobuzAppSecret,
		AuthToken: "stale-token",
	}

	provider := NewQobuzDirectProvider(server.URL, creds)
	stream, _, err := provider.GetStream(context.Background(), "12345", "", constants.QualityLossless)
	if err == nil {
		_ = stream.Close()
		t.Fatal("GetStream succeeded with a rejected token")
	}
	if !errors.Is(err, ErrQobuzTokenRejected) {
		t.Errorf("error = %v, want ErrQobuzTokenRejected", err)
	}
}

func TestQobuzDirectMetadataWorksWithoutSecret(t *testing.T) {
	// The app secret only signs downloads: browsing must still work without it.
	creds := QobuzCredentials{AppID: testQobuzAppID, AuthToken: "tok"}

	if !creds.CanAuthenticate() {
		t.Error("CanAuthenticate() = false, want true without a secret")
	}
	if creds.CanSign() {
		t.Error("CanSign() = true, want false without a secret")
	}

	provider := NewQobuzDirectProvider("https://example.test", creds)
	stream, _, err := provider.GetStream(context.Background(), "12345", "", constants.QualityLossless)
	if err == nil {
		_ = stream.Close()
		t.Fatal("GetStream succeeded without a signing secret")
	}
	if !errors.Is(err, ErrQobuzSecretMissing) {
		t.Errorf("error = %v, want ErrQobuzSecretMissing", err)
	}
}
