package catalog

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/cesargomez89/navidrums/internal/domain"
)

// crossProviderFallback serves metadata from the selected provider chain but
// lets any other configured provider type rescue a failed stream.
type crossProviderFallback struct {
	primary   Provider
	fallbacks []Provider
}

func (c *crossProviderFallback) GetStream(ctx context.Context, trackID string, isrc string, quality string) (io.ReadCloser, string, error) {
	var errs []error

	stream, mimeType, err := c.primary.GetStream(ctx, trackID, isrc, quality)
	if err == nil {
		return stream, mimeType, nil
	}
	errs = append(errs, fmt.Errorf("selected provider: %w", err))

	isrc = c.enrichISRC(ctx, trackID, isrc)

	if isrc != "" {
		stream, mimeType, err = c.primary.GetStream(ctx, trackID, isrc, quality)
		if err == nil {
			slog.Info("cross-provider: streamed via primary with enriched ISRC",
				"track_id", trackID, "isrc", isrc,
			)
			return stream, mimeType, nil
		}
		errs = append(errs, fmt.Errorf("selected provider with isrc %s: %w", isrc, err))
	}

	for _, fallback := range c.fallbacks {
		stream, mimeType, err = fallback.GetStream(ctx, trackID, isrc, quality)
		if err == nil {
			return stream, mimeType, nil
		}
		errs = append(errs, fmt.Errorf("fallback provider: %w", err))
	}

	joined := joinErrors(errs)
	slog.Error("cross-provider: all providers failed",
		"track_id", trackID, "isrc", isrc, "err", joined,
	)
	return nil, "", joined
}

func (c *crossProviderFallback) enrichISRC(ctx context.Context, trackID, isrc string) string {
	if isrc != "" {
		return isrc
	}

	if track, err := c.primary.GetTrack(ctx, trackID); err == nil && track.ISRC != "" {
		return track.ISRC
	}
	for _, fallback := range c.fallbacks {
		if track, err := fallback.GetTrack(ctx, trackID); err == nil && track.ISRC != "" {
			return track.ISRC
		}
	}

	slog.Warn("cross-provider: could not enrich ISRC", "track_id", trackID)
	return ""
}

func (c *crossProviderFallback) Search(ctx context.Context, query string, searchType string) (*domain.SearchResult, error) {
	return c.primary.Search(ctx, query, searchType)
}

func (c *crossProviderFallback) GetArtist(ctx context.Context, id string) (*domain.Artist, error) {
	return c.primary.GetArtist(ctx, id)
}

func (c *crossProviderFallback) GetAlbum(ctx context.Context, id string) (*domain.Album, error) {
	return c.primary.GetAlbum(ctx, id)
}

func (c *crossProviderFallback) GetPlaylist(ctx context.Context, id string) (*domain.Playlist, error) {
	return c.primary.GetPlaylist(ctx, id)
}

func (c *crossProviderFallback) GetTrack(ctx context.Context, id string) (*domain.CatalogTrack, error) {
	return c.primary.GetTrack(ctx, id)
}

func (c *crossProviderFallback) GetSimilarAlbums(ctx context.Context, id string) ([]domain.Album, error) {
	return c.primary.GetSimilarAlbums(ctx, id)
}

func (c *crossProviderFallback) GetSimilarArtists(ctx context.Context, id string) ([]domain.Artist, error) {
	return c.primary.GetSimilarArtists(ctx, id)
}

func (c *crossProviderFallback) GetLyrics(ctx context.Context, trackID string) (string, string, error) {
	return c.primary.GetLyrics(ctx, trackID)
}

func (c *crossProviderFallback) GetRecommendations(ctx context.Context, id string) ([]domain.CatalogTrack, error) {
	return c.primary.GetRecommendations(ctx, id)
}

// fallbackProviderTypes lists the other provider types that could rescue a
// stream, in the order they should be tried.
func fallbackProviderTypes(primary ProviderType) []ProviderType {
	types := make([]ProviderType, 0, len(ProviderTypes)-1)
	for _, pt := range ProviderTypes {
		if pt != primary {
			types = append(types, pt)
		}
	}
	return types
}

func (m *ProviderManager) hasProvidersOfType(pt ProviderType) bool {
	if m.providers == nil {
		return false
	}
	records, err := m.providers.ListByType(string(pt))
	if err != nil {
		return false
	}
	return len(records) > 0
}

var _ Provider = (*crossProviderFallback)(nil)
