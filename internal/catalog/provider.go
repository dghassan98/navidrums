package catalog

import (
	"context"
	"errors"
	"io"

	"github.com/cesargomez89/navidrums/internal/domain"
)

// ErrPreviewAsset reports that a provider handed back a preview clip instead of
// the full track. Callers should treat it as a failed download rather than
// keeping the file.
var ErrPreviewAsset = errors.New("provider returned a preview asset instead of the full track")

// Provider is the catalog seam. Qobuz is the only implementation, but the
// interface is what tests substitute for, so it stays.
type Provider interface {
	Search(ctx context.Context, query string, searchType string) (*domain.SearchResult, error)
	GetArtist(ctx context.Context, id string) (*domain.Artist, error)
	GetAlbum(ctx context.Context, id string) (*domain.Album, error)
	GetPlaylist(ctx context.Context, id string) (*domain.Playlist, error)
	GetTrack(ctx context.Context, id string) (*domain.CatalogTrack, error)
	GetStream(ctx context.Context, trackID string, isrc string, quality string) (io.ReadCloser, string, error)
	GetSimilarAlbums(ctx context.Context, id string) ([]domain.Album, error)
	GetSimilarArtists(ctx context.Context, id string) ([]domain.Artist, error)
	GetLyrics(ctx context.Context, trackID string) (string, string, error)
	GetRecommendations(ctx context.Context, id string) ([]domain.CatalogTrack, error)

	// Browse operations. Qobuz answers these from its editorial endpoints.
	GetFeatured(ctx context.Context, kind, genreID string, limit, offset int) ([]domain.Album, error)
	GetFeaturedPlaylists(ctx context.Context, genreID string, limit, offset int) ([]domain.Playlist, error)
	GetGenres(ctx context.Context) ([]domain.Genre, error)
	GetLabel(ctx context.Context, labelID string, limit, offset int) (*domain.Label, error)
}
