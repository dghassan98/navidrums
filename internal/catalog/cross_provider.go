package catalog

import (
	"context"
	"io"
	"log/slog"

	"github.com/cesargomez89/navidrums/internal/domain"
)

type crossProviderFallback struct {
	primary  Provider
	fallback Provider
}

func (c *crossProviderFallback) GetStream(ctx context.Context, trackID string, isrc string, quality string) (io.ReadCloser, string, error) {
	stream, mimeType, err := c.primary.GetStream(ctx, trackID, isrc, quality)
	if err == nil {
		return stream, mimeType, nil
	}

	isrc = c.enrichISRC(ctx, trackID, isrc)

	if isrc != "" {
		stream, mimeType, err = c.primary.GetStream(ctx, trackID, isrc, quality)
		if err == nil {
			slog.Info("cross-provider: streamed via primary with enriched ISRC",
				"track_id", trackID, "isrc", isrc,
			)
			return stream, mimeType, nil
		}
	}

	stream, mimeType, err = c.fallback.GetStream(ctx, trackID, isrc, quality)
	if err != nil {
		slog.Error("cross-provider: both providers failed",
			"track_id", trackID, "isrc", isrc, "err", err,
		)
	}
	return stream, mimeType, err
}

func (c *crossProviderFallback) enrichISRC(ctx context.Context, trackID, isrc string) string {
	if isrc != "" {
		return isrc
	}

	if track, err := c.primary.GetTrack(ctx, trackID); err == nil && track.ISRC != "" {
		return track.ISRC
	}
	if track, err := c.fallback.GetTrack(ctx, trackID); err == nil && track.ISRC != "" {
		return track.ISRC
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

func oppositeProviderType(pt ProviderType) ProviderType {
	if pt == ProviderTypeHifi {
		return ProviderTypeQobuz
	}
	return ProviderTypeHifi
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
