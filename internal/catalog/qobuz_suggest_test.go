package catalog

import (
	"encoding/json"
	"testing"
)

func TestQobuzArtistImageURL(t *testing.T) {
	tests := []struct {
		name  string
		image *QobuzArtistImage
		want  string
	}{
		{
			name:  "prefers large",
			image: &QobuzArtistImage{Small: "s.jpg", Medium: "m.jpg", Large: "l.jpg"},
			want:  "l.jpg",
		},
		{
			name:  "falls back to medium",
			image: &QobuzArtistImage{Small: "s.jpg", Medium: "m.jpg"},
			want:  "m.jpg",
		},
		{
			name:  "falls back to small",
			image: &QobuzArtistImage{Small: "s.jpg"},
			want:  "s.jpg",
		},
		{
			name:  "assembles a hash and format pair",
			image: &QobuzArtistImage{Hash: "abc123", Format: "jpg"},
			want:  qobuzArtistImageBase + "abc123.jpg",
		},
		{
			name:  "hash without format yields nothing",
			image: &QobuzArtistImage{Hash: "abc123"},
			want:  "",
		},
		{"empty image", &QobuzArtistImage{}, ""},
		{"nil image", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.image.URL(); got != tt.want {
				t.Errorf("URL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The payload below is the shape artist/getSimilarArtists actually returns:
// ready-made URLs, not the hash/format pair the search DTO expected. Decoding
// it into that DTO produced ".../artists/." and a broken image in the UI.
const similarArtistsPayload = `{"artists":{"total":8,"items":[
  {"id":636808,"name":"Lizzo","slug":"lizzo","albums_count":39,
   "picture":"https://static.qobuz.com/images/artists/covers/large/eb6d1bbf.jpg",
   "image":{"small":"https://static.qobuz.com/images/artists/covers/small/eb6d1bbf.jpg",
            "medium":"https://static.qobuz.com/images/artists/covers/medium/eb6d1bbf.jpg",
            "large":"https://static.qobuz.com/images/artists/covers/large/eb6d1bbf.jpg"}}
]}}`

func TestQobuzSimilarArtistsResponse(t *testing.T) {
	var resp QobuzSimilarArtistsResponse
	if err := json.Unmarshal([]byte(similarArtistsPayload), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(resp.Artists.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Artists.Items))
	}

	artist := resp.Artists.Items[0].ToDomain()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"id", artist.ID, "636808"},
		{"name", artist.Name, "Lizzo"},
		{"picture", artist.PictureURL, "https://static.qobuz.com/images/artists/covers/large/eb6d1bbf.jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestQobuzSuggestedArtistFallsBackToImage(t *testing.T) {
	// Some entries carry no top-level picture, only the image object.
	artist := (&QobuzSuggestedArtist{
		ID:    1,
		Name:  "Nobody",
		Image: &QobuzArtistImage{Large: "large.jpg"},
	}).ToDomain()

	if artist.PictureURL != "large.jpg" {
		t.Errorf("PictureURL = %q, want %q", artist.PictureURL, "large.jpg")
	}
}

// album/suggest names things differently to a search result: track_count, an
// artists list with no single artist, and dates.original for the year.
const suggestedAlbumsPayload = `{"albums":{"total":30,"items":[
  {"id":"bonaht0ukhdta","title":"The End of an Era","track_count":14,"duration":2400,
   "artists":[{"id":963398,"name":"Iggy Azalea","roles":["main-artist"]}],
   "label":{"id":302261,"name":"Bad Dreams Records"},
   "genre":{"id":133,"name":"Hip-Hop/Rap"},
   "dates":{"original":"2021-08-13","stream":"2021-08-13"},
   "image":{"small":"https://static.qobuz.com/c_230.jpg",
            "thumbnail":"https://static.qobuz.com/c_50.jpg",
            "large":"https://static.qobuz.com/c_600.jpg"}}
]}}`

func TestQobuzSuggestedAlbumsResponse(t *testing.T) {
	var resp QobuzSuggestedAlbumsResponse
	if err := json.Unmarshal([]byte(suggestedAlbumsPayload), &resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if len(resp.Albums.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(resp.Albums.Items))
	}

	album := resp.Albums.Items[0].ToDomain()

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"id", album.ID, "bonaht0ukhdta"},
		{"title", album.Title, "The End of an Era"},
		{"artist", album.Artist, "Iggy Azalea"},
		{"artist id", album.ArtistID, "963398"},
		{"cover", album.AlbumArtURL, "https://static.qobuz.com/c_600.jpg"},
		{"genre", album.Genre, "Hip-Hop/Rap"},
		{"label", album.Label, "Bad Dreams Records"},
		{"track count", album.TotalTracks, 14},
		{"year", album.Year, 2021},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
