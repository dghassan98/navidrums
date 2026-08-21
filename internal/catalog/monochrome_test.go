package catalog

import (
	"context"
	"encoding/base64"
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

const testAudioBody = "FLACAUDIOBYTES"

func TestMonochromeManifestFormat(t *testing.T) {
	tests := []struct {
		name    string
		quality string
		want    string
	}{
		{"hi res lossless", constants.QualityHiResLossless, "FLAC_HIRES"},
		{"lossless", constants.QualityLossless, "FLAC"},
		{"high", constants.QualityHigh, "AACLC"},
		{"low", constants.QualityLow, "HEAACV1"},
		{"unknown falls back to flac", "WHATEVER", "FLAC"},
		{"empty falls back to flac", "", "FLAC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := monochromeManifestFormat(tt.quality); got != tt.want {
				t.Errorf("monochromeManifestFormat(%q) = %q, want %q", tt.quality, got, tt.want)
			}
		})
	}
}

func TestManifestMimeType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		manifest    string
		want        string
	}{
		{"dash content type", "application/dash+xml", "", constants.MimeTypeDashXML},
		{"bts content type", "application/vnd.tidal.bts", "", constants.MimeTypeBTS},
		{"sniffed dash", "application/octet-stream", "<?xml version=\"1.0\"?><MPD/>", constants.MimeTypeDashXML},
		{"sniffed bts", "application/octet-stream", "  {\"urls\":[]}", constants.MimeTypeBTS},
		{"unrecognised keeps content type", "text/plain", "nonsense", "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectManifestMimeType(tt.contentType, []byte(tt.manifest))
			if got != tt.want {
				t.Errorf("detectManifestMimeType(%q, %q) = %q, want %q", tt.contentType, tt.manifest, got, tt.want)
			}
		})
	}
}

func TestNewMonochromeProviderBaseURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty uses default instance", "", constants.MonochromeDefaultURL},
		{"blank uses default instance", "   ", constants.MonochromeDefaultURL},
		{"trailing slash trimmed", "https://example.test/", "https://example.test"},
		{"custom instance kept", "https://example.test", "https://example.test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewMonochromeProvider(tt.input).BaseURL; got != tt.want {
				t.Errorf("NewMonochromeProvider(%q).BaseURL = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// monochromeTestServer serves the routes a Monochrome instance exposes. The
// caller decides what /trackManifests/ answers; the signed manifest and the
// audio it points at are fixed.
func monochromeTestServer(t *testing.T, manifests func(w http.ResponseWriter, r *http.Request, serverURL string)) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	mux := http.NewServeMux()

	mux.HandleFunc("/trackManifests/", func(w http.ResponseWriter, r *http.Request) {
		manifests(w, r, server.URL)
	})

	mux.HandleFunc("/manifest.mpd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dash+xml")
		fmt.Fprintf(w, "<?xml version=\"1.0\"?><MPD><Period><AdaptationSet><Representation>"+
			"<BaseURL>%s/audio.flac</BaseURL></Representation></AdaptationSet></Period></MPD>", server.URL)
	})

	mux.HandleFunc("/audio.flac", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", constants.MimeTypeFLAC)
		_, _ = io.WriteString(w, testAudioBody)
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

func manifestResponse(serverURL, presentation, previewReason string) string {
	return fmt.Sprintf(
		"{\"version\":\"2.10\",\"data\":{\"data\":{\"id\":\"1\",\"type\":\"trackManifests\",\"attributes\":"+
			"{\"uri\":\"%s/manifest.mpd\",\"trackPresentation\":\"%s\",\"previewReason\":\"%s\","+
			"\"formats\":[\"FLAC\"]}}}}",
		serverURL, presentation, previewReason,
	)
}

func readStream(t *testing.T, body io.ReadCloser) string {
	t.Helper()
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}
	return string(data)
}

func TestMonochromeGetStreamFullAsset(t *testing.T) {
	var mu sync.Mutex
	var gotQuery string
	server := monochromeTestServer(t, func(w http.ResponseWriter, r *http.Request, serverURL string) {
		mu.Lock()
		gotQuery = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, manifestResponse(serverURL, "FULL", ""))
	})

	provider := NewMonochromeProvider(server.URL)
	stream, mimeType, err := provider.GetStream(context.Background(), "1", "", constants.QualityLossless)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}

	if got := readStream(t, stream); got != testAudioBody {
		t.Errorf("stream body = %q, want %q", got, testAudioBody)
	}
	if mimeType != constants.MimeTypeFLAC {
		t.Errorf("mimeType = %q, want %q", mimeType, constants.MimeTypeFLAC)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{"id=1", "quality=LOSSLESS", "adaptive=false", "formats=FLAC"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("manifest query %q missing %q", gotQuery, want)
		}
	}
}

func TestMonochromeGetStreamRejectsPreview(t *testing.T) {
	server := monochromeTestServer(t, func(w http.ResponseWriter, r *http.Request, serverURL string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, manifestResponse(serverURL, "PREVIEW", "FULL_REQUIRES_SUBSCRIPTION"))
	})

	provider := NewMonochromeProvider(server.URL)
	stream, _, err := provider.GetStream(context.Background(), "1", "", constants.QualityLossless)
	if err == nil {
		_ = stream.Close()
		t.Fatal("GetStream returned a preview asset instead of an error")
	}
	if !errors.Is(err, ErrPreviewAsset) {
		t.Errorf("error = %v, want ErrPreviewAsset", err)
	}
	if !strings.Contains(err.Error(), "FULL_REQUIRES_SUBSCRIPTION") {
		t.Errorf("error %v does not report the preview reason", err)
	}
}

// legacyTestServer stands in for an instance still running the older hifi-api,
// which has no /trackManifests/ route.
func legacyTestServer(t *testing.T, presentation string) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	mux := http.NewServeMux()

	mux.HandleFunc("/trackManifests/", http.NotFound)

	mux.HandleFunc("/track/", func(w http.ResponseWriter, r *http.Request) {
		manifest := base64.StdEncoding.EncodeToString([]byte(
			fmt.Sprintf("{\"mimeType\":\"audio/flac\",\"urls\":[\"%s/audio.flac\"]}", server.URL),
		))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"data\":{\"assetPresentation\":%q,\"manifestMimeType\":%q,\"manifest\":%q}}",
			presentation, constants.MimeTypeBTS, manifest)
	})

	mux.HandleFunc("/audio.flac", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, testAudioBody)
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

func TestMonochromeGetStreamFallsBackToLegacyRoute(t *testing.T) {
	server := legacyTestServer(t, "FULL")

	provider := NewMonochromeProvider(server.URL)
	stream, mimeType, err := provider.GetStream(context.Background(), "1", "", constants.QualityLossless)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}
	if got := readStream(t, stream); got != testAudioBody {
		t.Errorf("stream body = %q, want %q", got, testAudioBody)
	}
	if mimeType != constants.MimeTypeFLAC {
		t.Errorf("mimeType = %q, want %q", mimeType, constants.MimeTypeFLAC)
	}
}

func TestMonochromeGetStreamRejectsLegacyPreview(t *testing.T) {
	server := legacyTestServer(t, "PREVIEW")

	provider := NewMonochromeProvider(server.URL)
	stream, _, err := provider.GetStream(context.Background(), "1", "", constants.QualityLossless)
	if err == nil {
		_ = stream.Close()
		t.Fatal("legacy route returned a preview asset instead of an error")
	}
	if !errors.Is(err, ErrPreviewAsset) {
		t.Errorf("error = %v, want ErrPreviewAsset", err)
	}
}

func TestMonochromeGetStreamRetriesLosslessAfterHiResRejection(t *testing.T) {
	var mu sync.Mutex
	var qualities []string
	server := monochromeTestServer(t, func(w http.ResponseWriter, r *http.Request, serverURL string) {
		quality := r.URL.Query().Get("quality")
		mu.Lock()
		qualities = append(qualities, quality)
		mu.Unlock()
		if quality == constants.QualityHiResLossless {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, manifestResponse(serverURL, "FULL", ""))
	})

	provider := NewMonochromeProvider(server.URL)
	stream, _, err := provider.GetStream(context.Background(), "1", "", constants.QualityHiResLossless)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}
	if got := readStream(t, stream); got != testAudioBody {
		t.Errorf("stream body = %q, want %q", got, testAudioBody)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{constants.QualityHiResLossless, constants.QualityLossless}
	if len(qualities) != len(want) {
		t.Fatalf("requested qualities = %v, want %v", qualities, want)
	}
	for i := range want {
		if qualities[i] != want[i] {
			t.Errorf("request %d quality = %q, want %q", i, qualities[i], want[i])
		}
	}
}

func TestMonochromeResolveTrackID(t *testing.T) {
	tests := []struct {
		name     string
		isrc     string
		exact    string
		freeText string
		want     string
	}{
		{
			name:     "exact isrc route wins",
			isrc:     "GBDUW0000053",
			exact:    "{\"data\":{\"items\":[{\"id\":1550546,\"isrc\":\"GBDUW0000053\"}]}}",
			freeText: "{\"data\":{\"items\":[{\"id\":999,\"isrc\":\"OTHER0000001\"}]}}",
			want:     "1550546",
		},
		{
			name:     "free text route used when exact route is empty",
			isrc:     "GBDUW0000053",
			exact:    "{\"data\":{\"items\":[]}}",
			freeText: "{\"data\":{\"items\":[{\"id\":1550546,\"isrc\":\"GBDUW0000053\"}]}}",
			want:     "1550546",
		},
		{
			name:     "free text mismatch is not trusted",
			isrc:     "GBDUW0000053",
			exact:    "{\"data\":{\"items\":[]}}",
			freeText: "{\"data\":{\"items\":[{\"id\":999,\"isrc\":\"OTHER0000001\"}]}}",
			want:     "fallback-id",
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
			mux.HandleFunc("/search/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("i") != "" {
					_, _ = io.WriteString(w, tt.exact)
					return
				}
				_, _ = io.WriteString(w, tt.freeText)
			})

			server := httptest.NewServer(mux)
			defer server.Close()

			provider := NewMonochromeProvider(server.URL)
			got := provider.resolveTrackID(context.Background(), "fallback-id", tt.isrc)
			if got != tt.want {
				t.Errorf("resolveTrackID(%q) = %q, want %q", tt.isrc, got, tt.want)
			}
		})
	}
}

func TestFallbackProviderTypes(t *testing.T) {
	tests := []struct {
		name    string
		primary ProviderType
		want    []ProviderType
	}{
		{"monochrome primary", ProviderTypeMonochrome, []ProviderType{ProviderTypeQobuzDirect, ProviderTypeHifi, ProviderTypeQobuz}},
		{"hifi primary", ProviderTypeHifi, []ProviderType{ProviderTypeMonochrome, ProviderTypeQobuzDirect, ProviderTypeQobuz}},
		{"qobuz primary", ProviderTypeQobuz, []ProviderType{ProviderTypeMonochrome, ProviderTypeQobuzDirect, ProviderTypeHifi}},
		{"qobuz-direct primary", ProviderTypeQobuzDirect, []ProviderType{ProviderTypeMonochrome, ProviderTypeHifi, ProviderTypeQobuz}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fallbackProviderTypes(tt.primary)
			if len(got) != len(tt.want) {
				t.Fatalf("fallbackProviderTypes(%q) = %v, want %v", tt.primary, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("fallbackProviderTypes(%q)[%d] = %q, want %q", tt.primary, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIsValidProviderType(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"monochrome", "monochrome", true},
		{"hifi", "hifi", true},
		{"qobuz", "qobuz", true},
		{"qobuz-direct", "qobuz-direct", true},
		{"unknown", "tidal", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidProviderType(tt.value); got != tt.want {
				t.Errorf("IsValidProviderType(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestNewProviderReturnsMonochrome(t *testing.T) {
	provider := NewProvider(ProviderTypeMonochrome, "https://example.test")
	if _, ok := provider.(*MonochromeProvider); !ok {
		t.Errorf("NewProvider(monochrome) = %T, want *MonochromeProvider", provider)
	}
}
