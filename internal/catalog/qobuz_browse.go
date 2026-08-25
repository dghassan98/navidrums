package catalog

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/cesargomez89/navidrums/internal/domain"
)

// QobuzFeaturedKinds lists every type album/getFeatured accepts. Qobuz answers
// an unknown type with a 400 whose message enumerates the valid set; this is
// that set, so a stale stored preference is dropped rather than requested.
var QobuzFeaturedKinds = []string{
	"new-releases",
	"recent-releases",
	"new-releases-full",
	"editor-picks",
	"press-awards",
	"most-streamed",
	"most-featured",
	"best-sellers",
	"ideal-discography",
	"qobuzissims",
	"harmonia-mundi",
	"universal-classic",
	"universal-jazz",
	"universal-jeunesse",
	"universal-chanson",
}

// IsValidFeaturedKind reports whether kind is one Qobuz will accept.
func IsValidFeaturedKind(kind string) bool {
	for _, k := range QobuzFeaturedKinds {
		if k == kind {
			return true
		}
	}
	return false
}

func (p *QobuzDirectProvider) GetFeatured(ctx context.Context, kind, genreID string, limit, offset int) ([]domain.Album, error) {
	if !IsValidFeaturedKind(kind) {
		return nil, fmt.Errorf("unknown qobuz featured type %q", kind)
	}

	params := browseParams(limit, offset)
	params.Set("type", kind)
	if genreID != "" {
		params.Set("genre_id", genreID)
	}

	var resp QobuzFeaturedAlbumsResponse
	if err := p.authedGet(ctx, "album/getFeatured", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get featured %s failed: %w", kind, err)
	}

	return resp.ToDomain(), nil
}

func (p *QobuzDirectProvider) GetFeaturedPlaylists(ctx context.Context, genreID string, limit, offset int) ([]domain.Playlist, error) {
	params := browseParams(limit, offset)
	params.Set("type", "editor-picks")
	if genreID != "" {
		// This endpoint takes a plural parameter, unlike album/getFeatured.
		params.Set("genre_ids", genreID)
	}

	var resp QobuzFeaturedPlaylistsResponse
	if err := p.authedGet(ctx, "playlist/getFeatured", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get featured playlists failed: %w", err)
	}

	return resp.ToDomain(), nil
}

// GetGenres returns the browse taxonomy, two levels deep.
//
// Qobuz has no single call for the tree: genre/list returns 13 top level
// genres, and genre/get?extra=subgenres does not honour the extra. Children
// come from genre/list?parent_id instead, so building the tree costs one call
// per top level genre. That is what the 24 hour cache in front of this is for.
//
// A child fetch that fails leaves that genre childless rather than failing the
// whole tree: a partial picker is far more useful than none.
func (p *QobuzDirectProvider) GetGenres(ctx context.Context) ([]domain.Genre, error) {
	params := browseParams(qobuzGenreLimit, 0)

	var resp QobuzGenreListResponse
	if err := p.authedGet(ctx, "genre/list", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get genres failed: %w", err)
	}

	genres := resp.ToDomain()
	for i := range genres {
		childParams := browseParams(qobuzGenreLimit, 0)
		childParams.Set("parent_id", genres[i].ID)

		var children QobuzGenreListResponse
		if err := p.authedGet(ctx, "genre/list", childParams, &children); err != nil {
			continue
		}
		genres[i].Children = children.ToDomain()
	}

	return genres, nil
}

func (p *QobuzDirectProvider) GetLabel(ctx context.Context, labelID string, limit, offset int) (*domain.Label, error) {
	params := browseParams(limit, offset)
	params.Set("label_id", labelID)
	params.Set("extra", "albums")

	var resp QobuzLabelResponse
	if err := p.authedGet(ctx, "label/get", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get label failed: %w", err)
	}

	return resp.ToDomain(), nil
}

// qobuzGenreLimit comfortably exceeds the 13 top level genres and the handful
// of children each has, so the tree never needs paging.
const qobuzGenreLimit = 100

func browseParams(limit, offset int) url.Values {
	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(offset))
	return params
}
