package catalog

import (
	"context"
	"errors"
	"io"

	"github.com/cesargomez89/navidrums/internal/domain"
)

// ErrPreviewAsset reports that a provider handed back a preview clip instead of
// the full track. Callers should try another instance rather than keep it.
var ErrPreviewAsset = errors.New("provider returned a preview asset instead of the full track")

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
}

type ProviderType string

const (
	ProviderTypeHifi        ProviderType = "hifi"
	ProviderTypeQobuz       ProviderType = "qobuz"
	ProviderTypeMonochrome  ProviderType = "monochrome"
	ProviderTypeQobuzDirect ProviderType = "qobuz-direct"
)

// DefaultProviderType is used whenever a stored selection is missing or
// invalid. Fresh installs are pointed at Monochrome by migration instead, so
// this only covers installs that predate provider selection.
const DefaultProviderType = ProviderTypeHifi

// ProviderTypes lists every supported provider type, in the order they are
// offered in Settings and tried as cross-provider fallbacks.
var ProviderTypes = []ProviderType{
	ProviderTypeMonochrome,
	ProviderTypeQobuzDirect,
	ProviderTypeHifi,
	ProviderTypeQobuz,
}

// IsValidProviderType reports whether value names a supported provider type.
func IsValidProviderType(value string) bool {
	for _, pt := range ProviderTypes {
		if ProviderType(value) == pt {
			return true
		}
	}
	return false
}
