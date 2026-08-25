package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fixture serves a captured Qobuz response from api-examples/qobuz-api.
func fixture(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("..", "..", "api-examples", "qobuz-api", name)
	data, err := os.ReadFile(path) //nolint:gosec // test fixture path is fixed
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func browseServer(t *testing.T, routes map[string]string) (*httptest.Server, *map[string][]string) {
	t.Helper()

	var mu sync.Mutex
	seen := make(map[string][]string)

	mux := http.NewServeMux()
	mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user_auth_token":"tok","user":{"credential":{"parameters":{"lossy_streaming":true}}}}`))
	})

	for route, body := range routes {
		route, body := route, body
		mux.HandleFunc("/"+route, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen[route] = append(seen[route], r.URL.RawQuery)
			mu.Unlock()
			_, _ = w.Write([]byte(body))
		})
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestGetFeaturedParsesAlbums(t *testing.T) {
	srv, seen := browseServer(t, map[string]string{
		"album/getFeatured": fixture(t, "featured-new-releases.json"),
	})

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	albums, err := p.GetFeatured(context.Background(), "new-releases", "", 5, 0)
	if err != nil {
		t.Fatalf("GetFeatured failed: %v", err)
	}

	if len(albums) != 3 {
		t.Fatalf("albums = %d, want 3", len(albums))
	}
	if albums[0].Title == "" {
		t.Error("album title was not parsed")
	}
	// Label and genre IDs are what make those fields clickable.
	if albums[0].LabelID == "" {
		t.Error("LabelID was not mapped")
	}
	if albums[0].GenreID == "" {
		t.Error("GenreID was not mapped")
	}

	queries := (*seen)["album/getFeatured"]
	if len(queries) != 1 {
		t.Fatalf("requests = %d, want 1", len(queries))
	}
	if !strings.Contains(queries[0], "type=new-releases") {
		t.Errorf("query %q missing the type", queries[0])
	}
	// An unfiltered call must omit genre_id rather than send it empty.
	if strings.Contains(queries[0], "genre_id") {
		t.Errorf("query %q sent genre_id when none was asked for", queries[0])
	}
}

func TestGetFeaturedPassesGenreFilter(t *testing.T) {
	srv, seen := browseServer(t, map[string]string{
		"album/getFeatured": fixture(t, "featured-genre-filtered.json"),
	})

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	if _, err := p.GetFeatured(context.Background(), "new-releases", "80", 5, 0); err != nil {
		t.Fatalf("GetFeatured failed: %v", err)
	}

	if q := (*seen)["album/getFeatured"][0]; !strings.Contains(q, "genre_id=80") {
		t.Errorf("query %q missing genre_id", q)
	}
}

func TestGetFeaturedRejectsUnknownKind(t *testing.T) {
	srv, seen := browseServer(t, map[string]string{
		"album/getFeatured": fixture(t, "featured-new-releases.json"),
	})

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	_, err := p.GetFeatured(context.Background(), "not-a-real-type", "", 5, 0)
	if err == nil {
		t.Fatal("expected an error for an unknown featured type")
	}
	// Qobuz answers an unknown type with a 400, so it must not be requested.
	if len((*seen)["album/getFeatured"]) != 0 {
		t.Error("an unknown type was sent upstream instead of being rejected locally")
	}
}

func TestGetFeaturedPlaylistsParsesNameAndImage(t *testing.T) {
	srv, _ := browseServer(t, map[string]string{
		"playlist/getFeatured": fixture(t, "featured-playlists.json"),
	})

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	playlists, err := p.GetFeaturedPlaylists(context.Background(), "", 5, 0)
	if err != nil {
		t.Fatalf("GetFeaturedPlaylists failed: %v", err)
	}

	if len(playlists) == 0 {
		t.Fatal("no playlists parsed")
	}
	// This endpoint calls the title `name`, unlike search.
	if playlists[0].Title == "" {
		t.Error("playlist title was not parsed from `name`")
	}
	if playlists[0].ImageURL == "" {
		t.Error("playlist image was not parsed from the images arrays")
	}
	if playlists[0].ProviderID == "" {
		t.Error("playlist id was not parsed")
	}
}

func TestGetGenresBuildsTreeFromParentID(t *testing.T) {
	// genre/list serves the top level, then children per parent_id: Qobuz has
	// no single call for the tree and ignores extra=subgenres.
	top := fixture(t, "genres.json")
	children := fixture(t, "genre-subgenres.json")

	var mu sync.Mutex
	var parentIDs []string

	mux := http.NewServeMux()
	mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user_auth_token":"tok","user":{"credential":{"parameters":{"lossy_streaming":true}}}}`))
	})
	mux.HandleFunc("/genre/list", func(w http.ResponseWriter, r *http.Request) {
		parent := r.URL.Query().Get("parent_id")
		mu.Lock()
		if parent != "" {
			parentIDs = append(parentIDs, parent)
		}
		mu.Unlock()

		if parent == "" {
			_, _ = w.Write([]byte(top))
			return
		}
		_, _ = w.Write([]byte(children))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	genres, err := p.GetGenres(context.Background())
	if err != nil {
		t.Fatalf("GetGenres failed: %v", err)
	}

	if len(genres) != 3 {
		t.Fatalf("top level genres = %d, want 3", len(genres))
	}
	if len(genres[0].Children) == 0 {
		t.Error("children were not attached to the top level genre")
	}
	if len(parentIDs) != len(genres) {
		t.Errorf("child fetches = %d, want one per top level genre (%d)", len(parentIDs), len(genres))
	}
}

func TestGetGenresSurvivesChildFailure(t *testing.T) {
	// A partial tree is far more useful than none.
	top := fixture(t, "genres.json")

	mux := http.NewServeMux()
	mux.HandleFunc("/user/login", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user_auth_token":"tok","user":{"credential":{"parameters":{"lossy_streaming":true}}}}`))
	})
	mux.HandleFunc("/genre/list", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("parent_id") != "" {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(top))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	genres, err := p.GetGenres(context.Background())
	if err != nil {
		t.Fatalf("GetGenres should not fail when only children fail: %v", err)
	}
	if len(genres) != 3 {
		t.Fatalf("top level genres = %d, want 3", len(genres))
	}
	for _, g := range genres {
		if len(g.Children) != 0 {
			t.Errorf("genre %s got children despite the child fetch failing", g.Name)
		}
	}
}

func TestGetLabelParsesAlbums(t *testing.T) {
	srv, seen := browseServer(t, map[string]string{
		"label/get": fixture(t, "label.json"),
	})

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	label, err := p.GetLabel(context.Background(), "1246569", 5, 0)
	if err != nil {
		t.Fatalf("GetLabel failed: %v", err)
	}

	if label.Name == "" {
		t.Error("label name was not parsed")
	}
	if label.AlbumsCount == 0 {
		t.Error("albums count was not parsed")
	}
	if len(label.Albums) == 0 {
		t.Error("label albums were not parsed")
	}

	if q := (*seen)["label/get"][0]; !strings.Contains(q, "extra=albums") {
		t.Errorf("query %q must ask for albums", q)
	}
}

func TestGetLabelPagination(t *testing.T) {
	srv, seen := browseServer(t, map[string]string{
		"label/get": fixture(t, "label.json"),
	})

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	if _, err := p.GetLabel(context.Background(), "1246569", 24, 48); err != nil {
		t.Fatalf("GetLabel failed: %v", err)
	}

	q := (*seen)["label/get"][0]
	if !strings.Contains(q, "limit=24") || !strings.Contains(q, "offset=48") {
		t.Errorf("query %q did not carry the paging window", q)
	}
}

// TestAlbumDetailMapsLabelAndGenreIDs covers the album page specifically: the
// search converter and the album-detail converter are separate, and only the
// detail one feeds /album/{id}, where the label and genre links live.
func TestAlbumDetailMapsLabelAndGenreIDs(t *testing.T) {
	srv, _ := browseServer(t, map[string]string{
		"album/get": fixture(t, "album.json"),
	})

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	album, err := p.GetAlbum(context.Background(), "abc")
	if err != nil {
		t.Fatalf("GetAlbum failed: %v", err)
	}

	if album.LabelID == "" {
		t.Error("LabelID was not mapped, so the album page cannot link to the label")
	}
	if album.GenreID == "" {
		t.Error("GenreID was not mapped, so the album page cannot link to the genre")
	}
	if album.Label == "" || album.Genre == "" {
		t.Error("label and genre names should still be populated")
	}
}

// TestAlbumTracksInheritAlbumFields covers a rendering bug: tracks embedded in
// an album response have no album object of their own, so without backfill
// every track row on an album page lost its cover art and album name.
func TestAlbumTracksInheritAlbumFields(t *testing.T) {
	srv, _ := browseServer(t, map[string]string{
		"album/get": fixture(t, "album.json"),
	})

	p := NewQobuzDirectProvider(srv.URL, testQobuzCredentials())
	album, err := p.GetAlbum(context.Background(), "abc")
	if err != nil {
		t.Fatalf("GetAlbum failed: %v", err)
	}
	if len(album.Tracks) == 0 {
		t.Fatal("no tracks parsed")
	}

	for i, track := range album.Tracks {
		if track.AlbumArtURL == "" {
			t.Errorf("track %d (%s) has no cover art", i, track.Title)
		}
		if track.Album == "" {
			t.Errorf("track %d (%s) has no album name", i, track.Title)
		}
		if track.AlbumID == "" {
			t.Errorf("track %d (%s) has no album id, so its album link is dead", i, track.Title)
		}
		if track.AlbumArtist == "" {
			t.Errorf("track %d (%s) has no album artist", i, track.Title)
		}
	}
}
