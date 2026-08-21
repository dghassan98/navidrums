package catalog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cesargomez89/navidrums/internal/constants"
)

// maxManifestSize caps the signed manifest body we are willing to read.
const maxManifestSize = 1 << 20

// errNoManifestRoute reports that an instance does not serve /trackManifests/,
// which is how the older hifi-api builds behave.
var errNoManifestRoute = errors.New("instance does not expose /trackManifests/")

// MonochromeProvider talks to a Monochrome API instance. Monochrome serves the
// same catalog routes as the hifi-api this project already speaks, so metadata
// is handled by the embedded HifiProvider; playback differs and is implemented
// here against /trackManifests/, which returns full-length assets instead of
// the 30 second previews the legacy /track/ route now hands back.
type MonochromeProvider struct {
	*HifiProvider
}

// NewMonochromeProvider builds a provider for the given instance URL, falling
// back to the default instance when none is configured.
func NewMonochromeProvider(baseURL string) *MonochromeProvider {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		trimmed = constants.MonochromeDefaultURL
	}
	return &MonochromeProvider{HifiProvider: NewHifiProvider(trimmed)}
}

// GetStream resolves the track and streams it from the instance, preferring the
// /trackManifests/ route and degrading gracefully for older instances.
func (p *MonochromeProvider) GetStream(ctx context.Context, trackID string, isrc string, quality string) (io.ReadCloser, string, error) {
	tid := p.resolveTrackID(ctx, trackID, isrc)

	stream, mimeType, err := p.streamFromTrackManifests(ctx, tid, quality)
	if err == nil {
		return stream, mimeType, nil
	}

	// A preview is never worth keeping. Fail instead of falling back so the
	// next configured instance gets its chance at the full asset.
	if errors.Is(err, ErrPreviewAsset) {
		return nil, "", err
	}

	// Instances still running the older hifi-api have no /trackManifests/.
	if errors.Is(err, errNoManifestRoute) {
		return p.streamFromTrackRoute(ctx, tid, quality, true)
	}

	// A rejected hi-res request usually means the instance's accounts have no
	// hi-res tier; lossless is still worth a try before giving up.
	if quality == constants.QualityHiResLossless {
		retryStream, retryMimeType, retryErr := p.streamFromTrackManifests(ctx, tid, constants.QualityLossless)
		if retryErr == nil {
			return retryStream, retryMimeType, nil
		}
	}

	return nil, "", err
}

// streamFromTrackManifests asks the instance for a signed manifest, fetches it
// and turns it into an audio stream.
func (p *MonochromeProvider) streamFromTrackManifests(ctx context.Context, trackID string, quality string) (io.ReadCloser, string, error) {
	params := url.Values{}
	params.Set("id", trackID)
	params.Set("quality", quality)
	params.Set("adaptive", "false")
	params.Add("formats", monochromeManifestFormat(quality))

	u := fmt.Sprintf("%s/trackManifests/?%s", p.BaseURL, params.Encode())

	var resp MonochromeTrackManifestResponse
	if err := p.get(ctx, u, &resp); err != nil {
		if statusCodeOf(err) == http.StatusNotFound {
			return nil, "", errNoManifestRoute
		}
		return nil, "", fmt.Errorf("monochrome track manifest request failed: %w", err)
	}

	attrs := resp.Attributes()
	if attrs == nil || attrs.URI == "" {
		return nil, "", errNoManifestRoute
	}

	if isPreviewPresentation(attrs.TrackPresentation) {
		reason := attrs.PreviewReason
		if reason == "" {
			reason = "unspecified"
		}
		return nil, "", fmt.Errorf("%w: track %s, reason %s", ErrPreviewAsset, trackID, reason)
	}

	manifest, manifestMimeType, err := p.fetchSignedManifest(ctx, attrs.URI)
	if err != nil {
		return nil, "", err
	}

	return p.streamFromManifest(ctx, manifestMimeType, manifest)
}

// fetchSignedManifest downloads the time-limited manifest the instance points
// at and reports the manifest format it holds.
func (p *MonochromeProvider) fetchSignedManifest(ctx context.Context, manifestURL string) ([]byte, string, error) {
	resp, err := p.openStreamURL(ctx, manifestURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch signed track manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	manifest, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read signed track manifest: %w", err)
	}

	return manifest, detectManifestMimeType(resp.Header.Get("Content-Type"), manifest), nil
}

// resolveTrackID maps an ISRC onto this instance's track ID. The exact lookup
// route is preferred; the free-text route is only trusted when the ISRC of the
// result matches, so an unrelated title match never gets downloaded.
func (p *MonochromeProvider) resolveTrackID(ctx context.Context, trackID string, isrc string) string {
	if isrc == "" {
		return trackID
	}

	if id := p.searchTrackIDByISRC(ctx, "i", isrc, false); id != "" {
		return id
	}
	if id := p.searchTrackIDByISRC(ctx, "s", isrc, true); id != "" {
		return id
	}

	return trackID
}

func (p *MonochromeProvider) searchTrackIDByISRC(ctx context.Context, param string, isrc string, requireMatch bool) string {
	u := fmt.Sprintf("%s/search/?%s=%s", p.BaseURL, param, url.QueryEscape(isrc))

	var resp APITracksSearchResponse
	if err := p.get(ctx, u, &resp); err != nil {
		return ""
	}

	for _, item := range resp.Data.Items {
		if requireMatch && !strings.EqualFold(item.ISRC, isrc) {
			continue
		}
		if id := formatID(item.ID); id != "" && id != "0" {
			return id
		}
	}

	return ""
}

// monochromeManifestFormat maps a Navidrums quality onto the TIDAL stream
// format the instance expects in the `formats` parameter.
func monochromeManifestFormat(quality string) string {
	switch quality {
	case constants.QualityHiResLossless:
		return "FLAC_HIRES"
	case constants.QualityLossless:
		return "FLAC"
	case constants.QualityHigh:
		return "AACLC"
	case constants.QualityLow:
		return "HEAACV1"
	default:
		return "FLAC"
	}
}

// detectManifestMimeType identifies a manifest from its content type, falling back to
// sniffing the body since some CDNs answer with a generic type.
func detectManifestMimeType(contentType string, manifest []byte) string {
	switch {
	case strings.Contains(contentType, "dash+xml"):
		return constants.MimeTypeDashXML
	case strings.Contains(contentType, "vnd.tidal.bts"):
		return constants.MimeTypeBTS
	}

	trimmed := bytes.TrimSpace(manifest)
	switch {
	case bytes.Contains(trimmed, []byte("<MPD")):
		return constants.MimeTypeDashXML
	case bytes.HasPrefix(trimmed, []byte("{")):
		return constants.MimeTypeBTS
	}

	return contentType
}

// isPreviewPresentation reports whether the instance handed back a preview
// clip rather than the full track.
func isPreviewPresentation(presentation string) bool {
	return strings.EqualFold(presentation, "PREVIEW")
}

var _ Provider = (*MonochromeProvider)(nil)
