package catalog

import (
	"strconv"

	"github.com/cesargomez89/navidrums/internal/domain"
)

// QobuzLoginResponse is the user/login envelope. A free account comes back with
// a null credential.parameters, which is how Qobuz signals it cannot stream.
type QobuzLoginResponse struct {
	UserAuthToken string `json:"user_auth_token"`
	User          struct {
		ID         int    `json:"id"`
		Email      string `json:"email"`
		Credential struct {
			Parameters *QobuzCredentialParameters `json:"parameters"`
		} `json:"credential"`
	} `json:"user"`
}

// QobuzCredentialParameters describes the subscription behind an account.
type QobuzCredentialParameters struct {
	Label                 string `json:"label"`
	ShortLabel            string `json:"short_label"`
	LosslessStreaming     bool   `json:"lossless_streaming"`
	HiresStreaming        bool   `json:"hires_streaming"`
	HiresPurchasesSteamed bool   `json:"hires_purchases_streaming"`
}

// QobuzFileURLResponse is the track/getFileUrl envelope. `URL` is a
// time-limited CDN link; when it is empty, `Restrictions` explains why.
type QobuzFileURLResponse struct {
	TrackID      int                    `json:"track_id"`
	FormatID     int                    `json:"format_id"`
	MimeType     string                 `json:"mime_type"`
	URL          string                 `json:"url"`
	Restrictions []QobuzFileRestriction `json:"restrictions"`
	Sampleable   bool                   `json:"sampleable"`
	BitDepth     int                    `json:"bit_depth"`
	SamplingRate float64                `json:"sampling_rate"`
}

// QobuzFileRestriction carries a CamelCase reason code, e.g. TrackRestrictedByRights.
type QobuzFileRestriction struct {
	Code string `json:"code"`
}

// QobuzDirectArtistResponse is the artist/get shape. It differs from the
// artist/page shape the proxy returns: `name` is a plain string here, and there
// are no top tracks or similar artists.
type QobuzDirectArtistResponse struct {
	ID      int        `json:"id"`
	Name    string     `json:"name"`
	Picture string     `json:"picture"`
	Image   QobuzImage `json:"image"`
	Albums  struct {
		Total int                    `json:"total"`
		Items []QobuzSearchAlbumItem `json:"items"`
	} `json:"albums"`
}

func (r *QobuzDirectArtistResponse) ToDomain() *domain.Artist {
	picURL := r.Image.Large
	if picURL == "" {
		picURL = r.Picture
	}

	albums := make([]domain.Album, 0, len(r.Albums.Items))
	for i := range r.Albums.Items {
		albums = append(albums, r.Albums.Items[i].ToDomain())
	}

	return &domain.Artist{
		ID:         strconv.Itoa(r.ID),
		Name:       r.Name,
		PictureURL: picURL,
		Albums:     albums,
	}
}

// QobuzDirectPlaylistResponse is the playlist/get shape.
type QobuzDirectPlaylistResponse struct {
	ID          int64                `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Images300   []string             `json:"images300"`
	Images      []string             `json:"images"`
	Tracks      QobuzTracksContainer `json:"tracks"`
}

func (r *QobuzDirectPlaylistResponse) ToDomain() *domain.Playlist {
	imageURL := ""
	if len(r.Images300) > 0 {
		imageURL = r.Images300[0]
	} else if len(r.Images) > 0 {
		imageURL = r.Images[0]
	}

	tracks := make([]domain.CatalogTrack, 0, len(r.Tracks.Items))
	for i := range r.Tracks.Items {
		tracks = append(tracks, r.Tracks.Items[i].ToDomain())
	}

	return &domain.Playlist{
		ProviderID:  strconv.FormatInt(r.ID, 10),
		Title:       r.Name,
		Description: r.Description,
		ImageURL:    imageURL,
		Tracks:      tracks,
	}
}

// QobuzSuggestedAlbumsResponse is the album/suggest envelope.
type QobuzSuggestedAlbumsResponse struct {
	Albums struct {
		Items []QobuzSuggestedAlbum `json:"items"`
		Total int                   `json:"total"`
	} `json:"albums"`
}

// QobuzSimilarArtistsResponse is the artist/getSimilarArtists envelope.
type QobuzSimilarArtistsResponse struct {
	Artists struct {
		Items []QobuzSuggestedArtist `json:"items"`
		Total int                    `json:"total"`
	} `json:"artists"`
}

// QobuzSuggestedArtist is an artist/getSimilarArtists item. `picture` already
// holds a full URL, with `image` as the fallback.
type QobuzSuggestedArtist struct {
	ID      int               `json:"id"`
	Name    string            `json:"name"`
	Picture string            `json:"picture"`
	Image   *QobuzArtistImage `json:"image"`
}

func (a *QobuzSuggestedArtist) ToDomain() domain.Artist {
	pictureURL := a.Picture
	if pictureURL == "" {
		pictureURL = a.Image.URL()
	}

	return domain.Artist{
		ID:         strconv.Itoa(a.ID),
		Name:       a.Name,
		PictureURL: pictureURL,
	}
}

// QobuzArtistImage covers both artist image shapes Qobuz returns. The official
// API sends ready-made URLs (small/medium/large); the proxy's artist pages send
// a hash and format that have to be assembled into a URL.
type QobuzArtistImage struct {
	Small  string `json:"small"`
	Medium string `json:"medium"`
	Large  string `json:"large"`
	Hash   string `json:"hash"`
	Format string `json:"format"`
}

// URL returns the largest usable image URL, or "" when the payload carried none.
func (i *QobuzArtistImage) URL() string {
	if i == nil {
		return ""
	}
	for _, candidate := range []string{i.Large, i.Medium, i.Small} {
		if candidate != "" {
			return candidate
		}
	}
	if i.Hash != "" && i.Format != "" {
		return qobuzArtistImageBase + i.Hash + "." + i.Format
	}
	return ""
}

// QobuzSuggestedAlbum is an album/suggest item. It is not shaped like a search
// result: the track count and release date are named differently and there is
// no single `artist`, only an `artists` list.
type QobuzSuggestedAlbum struct {
	ID         string             `json:"id"`
	Title      string             `json:"title"`
	TrackCount int                `json:"track_count"`
	Duration   int                `json:"duration"`
	Image      QobuzImage         `json:"image"`
	Artists    []QobuzAlbumArtist `json:"artists"`
	Label      QobuzLabel         `json:"label"`
	Genre      QobuzGenre         `json:"genre"`
	Dates      struct {
		Original string `json:"original"`
		Stream   string `json:"stream"`
	} `json:"dates"`
}

func (a *QobuzSuggestedAlbum) ToDomain() domain.Album {
	album := domain.Album{
		ID:          a.ID,
		Title:       a.Title,
		AlbumArtURL: a.Image.Large,
		Genre:       a.Genre.Name,
		Label:       a.Label.Name,
		Year:        parseYear(firstNonBlank(a.Dates.Original, a.Dates.Stream)),
		TotalTracks: a.TrackCount,
	}

	for _, artist := range a.Artists {
		album.ArtistIDs = append(album.ArtistIDs, strconv.Itoa(artist.ID))
		album.Artists = append(album.Artists, artist.Name)
	}
	if len(a.Artists) > 0 {
		album.ArtistID = strconv.Itoa(a.Artists[0].ID)
		album.Artist = a.Artists[0].Name
	}

	return album
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// qobuzArtistImageBase is where hash/format artist pictures are hosted.
const qobuzArtistImageBase = "https://static.qobuz.com/images/artists/"

// QobuzArtistPageResponse is the artist/page envelope. artist/get carries no
// top tracks at all, so an artist page built from it alone always reported
// "0 top tracks"; this route supplies them, plus a portrait when artist/get
// has none.
type QobuzArtistPageResponse struct {
	ID        int                 `json:"id"`
	Name      QobuzNameObject     `json:"name"`
	Images    QobuzArtistImages   `json:"images"`
	TopTracks []QobuzTopTrackItem `json:"top_tracks"`
}

// PortraitURL returns the artist portrait, or "" when Qobuz holds none.
func (r *QobuzArtistPageResponse) PortraitURL() string {
	if r.Images.Portrait == nil || r.Images.Portrait.Hash == "" {
		return ""
	}
	return qobuzArtistImageBase + r.Images.Portrait.Hash + "." + r.Images.Portrait.Format
}

// ToTopTracks converts the page's top tracks into catalog tracks.
func (r *QobuzArtistPageResponse) ToTopTracks() []domain.CatalogTrack {
	tracks := make([]domain.CatalogTrack, 0, len(r.TopTracks))
	for i := range r.TopTracks {
		tracks = append(tracks, r.TopTracks[i].ToDomain())
	}
	return tracks
}
