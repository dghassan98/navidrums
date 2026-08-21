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
