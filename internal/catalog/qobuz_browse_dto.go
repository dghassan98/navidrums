package catalog

// Response shapes for the Qobuz editorial endpoints. Captured samples live in
// api-examples/qobuz-api/: featured-new-releases.json, featured-playlists.json,
// genres.json, genre-subgenres.json and label.json.

// QobuzFeaturedAlbumsResponse is what album/getFeatured returns. The album
// objects are the same shape search returns, so the existing item DTO is
// reused rather than duplicated.
type QobuzFeaturedAlbumsResponse struct {
	Albums struct {
		Items  []QobuzSearchAlbumItem `json:"items"`
		Total  int                    `json:"total"`
		Limit  int                    `json:"limit"`
		Offset int                    `json:"offset"`
	} `json:"albums"`
}

// QobuzFeaturedPlaylistsResponse is what playlist/getFeatured returns. Its
// playlist objects differ from the ones search returns: the title is `name`,
// and images arrive as parallel arrays of URLs by size rather than an object.
type QobuzFeaturedPlaylistsResponse struct {
	Playlists struct {
		Items  []QobuzFeaturedPlaylistItem `json:"items"`
		Total  int                         `json:"total"`
		Limit  int                         `json:"limit"`
		Offset int                         `json:"offset"`
	} `json:"playlists"`
}

type QobuzFeaturedPlaylistItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Slug        string   `json:"slug"`
	Images      []string `json:"images"`
	Images150   []string `json:"images150"`
	Images300   []string `json:"images300"`
	Owner       struct {
		Name string `json:"name"`
		ID   int64  `json:"id"`
	} `json:"owner"`
	ID          int64 `json:"id"`
	TracksCount int   `json:"tracks_count"`
	Duration    int   `json:"duration"`
}

// QobuzGenreListResponse is what genre/list returns, both for the top level
// and, with parent_id set, for one genre's children.
type QobuzGenreListResponse struct {
	Genres struct {
		Items  []QobuzGenreItem `json:"items"`
		Total  int              `json:"total"`
		Limit  int              `json:"limit"`
		Offset int              `json:"offset"`
	} `json:"genres"`
}

type QobuzGenreItem struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Slug  string `json:"slug"`
	Path  []int  `json:"path"`
	ID    int    `json:"id"`
}

// QobuzLabelResponse is what label/get?extra=albums returns.
type QobuzLabelResponse struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Albums      struct {
		Items  []QobuzSearchAlbumItem `json:"items"`
		Total  int                    `json:"total"`
		Limit  int                    `json:"limit"`
		Offset int                    `json:"offset"`
	} `json:"albums"`
	ID          int64 `json:"id"`
	AlbumsCount int   `json:"albums_count"`
}
