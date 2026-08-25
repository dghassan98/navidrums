package catalog

import (
	"strconv"

	"github.com/cesargomez89/navidrums/internal/domain"
)

func (r *QobuzFeaturedAlbumsResponse) ToDomain() []domain.Album {
	albums := make([]domain.Album, 0, len(r.Albums.Items))
	for i := range r.Albums.Items {
		albums = append(albums, r.Albums.Items[i].ToDomain())
	}
	return albums
}

func (item *QobuzFeaturedPlaylistItem) ToDomain() domain.Playlist {
	return domain.Playlist{
		ProviderID:  strconv.FormatInt(item.ID, 10),
		Title:       item.Name,
		Description: item.Description,
		ImageURL:    firstImage(item.Images300, item.Images150, item.Images),
	}
}

func (r *QobuzFeaturedPlaylistsResponse) ToDomain() []domain.Playlist {
	playlists := make([]domain.Playlist, 0, len(r.Playlists.Items))
	for i := range r.Playlists.Items {
		playlists = append(playlists, r.Playlists.Items[i].ToDomain())
	}
	return playlists
}

// firstImage returns the first URL from the first non-empty list, so a playlist
// falls back to a smaller cover rather than rendering no image at all.
func firstImage(lists ...[]string) string {
	for _, list := range lists {
		if len(list) > 0 && list[0] != "" {
			return list[0]
		}
	}
	return ""
}

func (item *QobuzGenreItem) ToDomain() domain.Genre {
	return domain.Genre{
		ID:    strconv.Itoa(item.ID),
		Name:  item.Name,
		Slug:  item.Slug,
		Color: item.Color,
	}
}

func (r *QobuzGenreListResponse) ToDomain() []domain.Genre {
	genres := make([]domain.Genre, 0, len(r.Genres.Items))
	for i := range r.Genres.Items {
		genres = append(genres, r.Genres.Items[i].ToDomain())
	}
	return genres
}

func (r *QobuzLabelResponse) ToDomain() *domain.Label {
	albums := make([]domain.Album, 0, len(r.Albums.Items))
	for i := range r.Albums.Items {
		albums = append(albums, r.Albums.Items[i].ToDomain())
	}

	return &domain.Label{
		ID:          strconv.FormatInt(r.ID, 10),
		Name:        r.Name,
		Description: r.Description,
		ImageURL:    r.Image,
		AlbumsCount: r.AlbumsCount,
		Albums:      albums,
	}
}
