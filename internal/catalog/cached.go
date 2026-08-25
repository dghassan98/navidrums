package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/store"
)

type Cache interface {
	GetCache(key string) ([]byte, error)
	SetCache(key string, data []byte, ttl time.Duration) error
	ClearCache() error
}

type CachedProvider struct {
	provider Provider
	cache    Cache
	cacheTTL time.Duration
}

func NewCachedProvider(provider Provider, cache Cache, cacheTTL time.Duration) *CachedProvider {
	return &CachedProvider{
		provider: provider,
		cache:    cache,
		cacheTTL: cacheTTL,
	}
}

// cacheKeyPrefix namespaces every entry. It matches the prefix the qobuz-direct
// chain already used, so caches stay warm across the upgrade and cannot collide
// with entries left behind by the providers that were removed.
const cacheKeyPrefix = "qobuz-direct:"

// cacheSchemaVersion invalidates every cached entry when the shape of what is
// cached changes.
//
// Entries are stored as marshalled domain objects, so adding a field that the
// converters now populate does not fix already-cached responses: a rebuilt
// binary keeps serving the old JSON until the TTL expires. That has bitten
// twice — album label/genre IDs, then track cover art — each time looking like
// the fix had not worked.
//
// Bump this whenever a converter starts populating a field that is cached.
//
//	v1 - initial
//	v2 - album tracks inherit album id, name, artist and cover art
const cacheSchemaVersion = "v2"

// Browse responses age very differently from catalog lookups, so they carry
// their own TTLs rather than the configured default.
const (
	genresCacheTTL   = 24 * time.Hour
	featuredCacheTTL = time.Hour
	labelCacheTTL    = time.Hour
)

func (c *CachedProvider) key(format string, args ...any) string {
	return cacheKeyPrefix + cacheSchemaVersion + ":" + fmt.Sprintf(format, args...)
}

// cached runs fetch through the cache under key, decoding into T. A cache miss,
// a decode failure or an unmarshalable result all fall through to fetch rather
// than failing: a broken cache entry must never break the request.
func cached[T any](c *CachedProvider, key string, ttl time.Duration, fetch func() (T, error)) (T, error) {
	var zero T

	data, err := c.cache.GetCache(key)
	if err != nil {
		return zero, err
	}
	if data != nil {
		var out T
		if json.Unmarshal(data, &out) == nil {
			return out, nil
		}
	}

	out, err := fetch()
	if err != nil {
		return zero, err
	}

	if encoded, marshalErr := json.Marshal(out); marshalErr == nil {
		_ = c.cache.SetCache(key, encoded, ttl)
	}

	return out, nil
}

func (c *CachedProvider) Search(ctx context.Context, query string, searchType string) (*domain.SearchResult, error) {
	cacheKey := c.key("search:%s:%s", searchType, query)

	data, err := c.cache.GetCache(cacheKey)
	if err != nil {
		return nil, err
	}
	if data != nil {
		var result domain.SearchResult
		if unmarshalErr := json.Unmarshal(data, &result); unmarshalErr == nil {
			return &result, nil
		}
	}

	result, err := c.provider.Search(ctx, query, searchType)
	if err != nil {
		return nil, err
	}

	if data, marshalErr := json.Marshal(result); marshalErr == nil {
		_ = c.cache.SetCache(cacheKey, data, c.cacheTTL)
	}

	return result, nil
}

func (c *CachedProvider) GetArtist(ctx context.Context, id string) (*domain.Artist, error) {
	cacheKey := c.key("artist:%s", id)

	data, err := c.cache.GetCache(cacheKey)
	if err != nil {
		return nil, err
	}
	if data != nil {
		var artist domain.Artist
		if unmarshalErr := json.Unmarshal(data, &artist); unmarshalErr == nil {
			return &artist, nil
		}
	}

	artist, err := c.provider.GetArtist(ctx, id)
	if err != nil {
		return nil, err
	}

	if data, marshalErr := json.Marshal(artist); marshalErr == nil {
		_ = c.cache.SetCache(cacheKey, data, c.cacheTTL)
	}

	return artist, nil
}

func (c *CachedProvider) GetAlbum(ctx context.Context, id string) (*domain.Album, error) {
	cacheKey := c.key("album:%s", id)

	data, err := c.cache.GetCache(cacheKey)
	if err != nil {
		return nil, err
	}
	if data != nil {
		var album domain.Album
		if unmarshalErr := json.Unmarshal(data, &album); unmarshalErr == nil {
			return &album, nil
		}
	}

	album, err := c.provider.GetAlbum(ctx, id)
	if err != nil {
		return nil, err
	}

	if data, marshalErr := json.Marshal(album); marshalErr == nil {
		_ = c.cache.SetCache(cacheKey, data, c.cacheTTL)
	}

	return album, nil
}

func (c *CachedProvider) GetPlaylist(ctx context.Context, id string) (*domain.Playlist, error) {
	cacheKey := c.key("playlist:%s", id)

	data, err := c.cache.GetCache(cacheKey)
	if err != nil {
		return nil, err
	}
	if data != nil {
		var playlist domain.Playlist
		if unmarshalErr := json.Unmarshal(data, &playlist); unmarshalErr == nil {
			return &playlist, nil
		}
	}

	playlist, err := c.provider.GetPlaylist(ctx, id)
	if err != nil {
		return nil, err
	}

	if data, marshalErr := json.Marshal(playlist); marshalErr == nil {
		_ = c.cache.SetCache(cacheKey, data, c.cacheTTL)
	}

	return playlist, nil
}

func (c *CachedProvider) GetTrack(ctx context.Context, id string) (*domain.CatalogTrack, error) {
	cacheKey := c.key("track:%s", id)

	data, err := c.cache.GetCache(cacheKey)
	if err != nil {
		return nil, err
	}
	if data != nil {
		var track domain.CatalogTrack
		if unmarshalErr := json.Unmarshal(data, &track); unmarshalErr == nil {
			return &track, nil
		}
	}

	track, err := c.provider.GetTrack(ctx, id)
	if err != nil {
		return nil, err
	}

	if data, marshalErr := json.Marshal(track); marshalErr == nil {
		_ = c.cache.SetCache(cacheKey, data, c.cacheTTL)
	}

	return track, nil
}

func (c *CachedProvider) GetStream(ctx context.Context, trackID string, isrc string, quality string) (io.ReadCloser, string, error) {
	return c.provider.GetStream(ctx, trackID, isrc, quality)
}

func (c *CachedProvider) GetSimilarAlbums(ctx context.Context, id string) ([]domain.Album, error) {
	cacheKey := c.key("similar-albums:%s", id)

	data, err := c.cache.GetCache(cacheKey)
	if err != nil {
		return nil, err
	}
	if data != nil {
		var albums []domain.Album
		if unmarshalErr := json.Unmarshal(data, &albums); unmarshalErr == nil {
			return albums, nil
		}
	}

	albums, err := c.provider.GetSimilarAlbums(ctx, id)
	if err != nil {
		return nil, err
	}

	if len(albums) > 0 {
		if data, marshalErr := json.Marshal(albums); marshalErr == nil {
			_ = c.cache.SetCache(cacheKey, data, c.cacheTTL)
		}
	}

	return albums, nil
}

func (c *CachedProvider) GetSimilarArtists(ctx context.Context, id string) ([]domain.Artist, error) {
	cacheKey := c.key("similar-artists:%s", id)

	data, err := c.cache.GetCache(cacheKey)
	if err != nil {
		return nil, err
	}
	if data != nil {
		var artists []domain.Artist
		if unmarshalErr := json.Unmarshal(data, &artists); unmarshalErr == nil {
			return artists, nil
		}
	}

	artists, err := c.provider.GetSimilarArtists(ctx, id)
	if err != nil {
		return nil, err
	}

	if len(artists) > 0 {
		if data, marshalErr := json.Marshal(artists); marshalErr == nil {
			_ = c.cache.SetCache(cacheKey, data, c.cacheTTL)
		}
	}

	return artists, nil
}

func (c *CachedProvider) GetRecommendations(ctx context.Context, id string) ([]domain.CatalogTrack, error) {
	cacheKey := c.key("track-recommendations:%s", id)

	data, err := c.cache.GetCache(cacheKey)
	if err != nil {
		return nil, err
	}
	if data != nil {
		var tracks []domain.CatalogTrack
		if unmarshalErr := json.Unmarshal(data, &tracks); unmarshalErr == nil {
			return tracks, nil
		}
	}

	tracks, err := c.provider.GetRecommendations(ctx, id)
	if err != nil {
		return nil, err
	}

	if len(tracks) > 0 {
		if data, marshalErr := json.Marshal(tracks); marshalErr == nil {
			_ = c.cache.SetCache(cacheKey, data, c.cacheTTL)
		}
	}

	return tracks, nil
}

func (c *CachedProvider) GetLyrics(ctx context.Context, trackID string) (string, string, error) {
	return c.provider.GetLyrics(ctx, trackID)
}

func (c *CachedProvider) GetFeatured(ctx context.Context, kind, genreID string, limit, offset int) ([]domain.Album, error) {
	key := c.key("featured:%s:%s:%d:%d", kind, genreID, limit, offset)
	return cached(c, key, featuredCacheTTL, func() ([]domain.Album, error) {
		return c.provider.GetFeatured(ctx, kind, genreID, limit, offset)
	})
}

func (c *CachedProvider) GetFeaturedPlaylists(ctx context.Context, genreID string, limit, offset int) ([]domain.Playlist, error) {
	key := c.key("featured-playlists:%s:%d:%d", genreID, limit, offset)
	return cached(c, key, featuredCacheTTL, func() ([]domain.Playlist, error) {
		return c.provider.GetFeaturedPlaylists(ctx, genreID, limit, offset)
	})
}

func (c *CachedProvider) GetGenres(ctx context.Context) ([]domain.Genre, error) {
	return cached(c, c.key("genres"), genresCacheTTL, func() ([]domain.Genre, error) {
		return c.provider.GetGenres(ctx)
	})
}

func (c *CachedProvider) GetLabel(ctx context.Context, labelID string, limit, offset int) (*domain.Label, error) {
	key := c.key("label:%s:%d:%d", labelID, limit, offset)
	return cached(c, key, labelCacheTTL, func() (*domain.Label, error) {
		return c.provider.GetLabel(ctx, labelID, limit, offset)
	})
}

func (c *CachedProvider) ClearCache() error {
	return c.cache.ClearCache()
}

var _ Provider = (*CachedProvider)(nil)

type storeCache struct {
	store *store.DB
}

func (s *storeCache) GetCache(key string) ([]byte, error) {
	return s.store.GetCache(key)
}

func (s *storeCache) SetCache(key string, data []byte, ttl time.Duration) error {
	return s.store.SetCache(key, data, ttl)
}

func (s *storeCache) ClearCache() error {
	return s.store.ClearCache()
}

var _ Cache = (*storeCache)(nil)
