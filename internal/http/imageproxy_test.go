package httpapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxiedImageURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "qobuz artwork is proxied",
			raw:  "https://static.qobuz.com/images/artists/covers/large/abc.jpg",
			want: imageProxyPath + "?url=https%3A%2F%2Fstatic.qobuz.com%2Fimages%2Fartists%2Fcovers%2Flarge%2Fabc.jpg",
		},
		{
			name: "a removed provider's CDN is no longer proxied",
			raw:  "https://resources.tidal.com/images/a/b/640x640.jpg",
			want: "https://resources.tidal.com/images/a/b/640x640.jpg",
		},
		{"empty falls back to the placeholder", "", placeholderImagePath},
		{"unknown host passes through", "https://example.test/a.jpg", "https://example.test/a.jpg"},
		{"local placeholder passes through", placeholderImagePath, placeholderImagePath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProxiedImageURL(tt.raw); got != tt.want {
				t.Errorf("ProxiedImageURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestImageProxyRejectsDisallowedTargets(t *testing.T) {
	h := &Handler{}

	tests := []struct {
		name string
		url  string
		want int
	}{
		{"missing url", "", http.StatusBadRequest},
		{"internal address", "http://127.0.0.1:8080/admin", http.StatusForbidden},
		{"private network", "https://192.168.1.1/x.jpg", http.StatusForbidden},
		{"arbitrary host", "https://example.test/x.jpg", http.StatusForbidden},
		{"plain http on an allowed host", "http://static.qobuz.com/x.jpg", http.StatusForbidden},
		{"file scheme", "file:///etc/passwd", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "/img"
			if tt.url != "" {
				target += "?url=" + tt.url
			}
			r := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()

			h.ImageProxy(w, r)

			if w.Code != tt.want {
				t.Errorf("status = %d, want %d (body %q)", w.Code, tt.want, strings.TrimSpace(w.Body.String()))
			}
		})
	}
}
