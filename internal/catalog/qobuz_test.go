package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQobuzQualityCode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"hi res lossless", "HI_RES_LOSSLESS", 27},
		{"lossless", "LOSSLESS", 6},
		{"high", "HIGH", 5},
		{"low", "LOW", 1},
		{"default for unknown", "UNKNOWN", 6},
		{"default for empty", "", 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := qobuzQualityCode(tt.input)
			if result != tt.expected {
				t.Errorf("qobuzQualityCode(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestResolveQobuzAudioQuality(t *testing.T) {
	tests := []struct {
		name     string
		hires    bool
		bitDepth int
		expected string
	}{
		{"hi res with 24 bit", true, 24, "HI_RES_LOSSLESS"},
		{"hi res with 32 bit", true, 32, "HI_RES_LOSSLESS"},
		{"not hires but 24 bit", false, 24, "LOSSLESS"},
		{"hires but 16 bit", true, 16, "LOSSLESS"},
		{"16 bit lossless", false, 16, "LOSSLESS"},
		{"low quality", false, 8, "LOW"},
		{"zero values", false, 0, "LOW"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveQobuzAudioQuality(tt.hires, tt.bitDepth)
			if result != tt.expected {
				t.Errorf("resolveQobuzAudioQuality(%v, %d) = %s, want %s", tt.hires, tt.bitDepth, result, tt.expected)
			}
		})
	}
}

func TestQobuzResolveTrackID_NumericFallback(t *testing.T) {
	p := &QobuzProvider{}

	tid, err := p.resolveTrackID(context.Background(), "12345", "")
	if err != nil {
		t.Fatalf("resolveTrackID failed: %v", err)
	}
	if tid != 12345 {
		t.Errorf("resolveTrackID = %d, want 12345", tid)
	}
}

func TestQobuzResolveTrackID_InvalidNumeric(t *testing.T) {
	p := &QobuzProvider{}

	_, err := p.resolveTrackID(context.Background(), "not-a-number", "")
	if err == nil {
		t.Fatal("expected error for invalid track ID")
	}
}

func TestQobuzResolveTrackID_ISRCLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("isrc") == "USABC1234567" {
			json.NewEncoder(w).Encode(QobuzTrackLookupResponse{
				Success: true,
				Data:    &QobuzTrackLookupData{ID: 99999},
			})
			return
		}
		json.NewEncoder(w).Encode(QobuzTrackLookupResponse{
			Success: false,
			Data:    nil,
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	t.Run("successful ISRC lookup", func(t *testing.T) {
		tid, err := p.resolveTrackID(context.Background(), "", "USABC1234567")
		if err != nil {
			t.Fatalf("resolveTrackID failed: %v", err)
		}
		if tid != 99999 {
			t.Errorf("resolveTrackID = %d, want 99999", tid)
		}
	})

	t.Run("ISRC lookup not found", func(t *testing.T) {
		_, err := p.resolveTrackID(context.Background(), "", "NOTFOUND")
		if err == nil {
			t.Fatal("expected error for unresolved ISRC")
		}
	})
}

func TestQobuzGetStream_HTTPStatusCheck(t *testing.T) {
	downloadCalled := false
	var streamURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download-music" {
			downloadCalled = true
			json.NewEncoder(w).Encode(QobuzDownloadResponse{
				Success: true,
				Data:    &QobuzDownloadData{URL: streamURL},
			})
			return
		}
		if r.URL.Path == "/stream" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	}))
	streamURL = srv.URL + "/stream"
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	_, _, err := p.GetStream(context.Background(), "1", "", "LOSSLESS")
	if err == nil {
		t.Fatal("expected error for non-200 stream response")
	}

	if !downloadCalled {
		t.Error("download endpoint was never called")
	}
}

func TestQobuzGetStream_SuccessfulStream(t *testing.T) {
	var streamURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/download-music" {
			json.NewEncoder(w).Encode(QobuzDownloadResponse{
				Success: true,
				Data:    &QobuzDownloadData{URL: streamURL},
			})
			return
		}
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "audio/flac")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("fake flac data"))
			return
		}
	}))
	streamURL = srv.URL + "/stream"
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	body, mime, err := p.GetStream(context.Background(), "1", "", "LOSSLESS")
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}
	defer body.Close()

	if mime != "audio/flac" {
		t.Errorf("mime type = %q, want %q", mime, "audio/flac")
	}
}

func TestQobuzGetArtist_RejectsFailedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzArtistResponse{
			Success: false,
			Data:    QobuzArtistData{},
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	_, err := p.GetArtist(context.Background(), "1")
	if err == nil {
		t.Fatal("expected error for unsuccessful artist response")
	}
}

func TestQobuzSearch_RejectsFailedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzSearchResponse{
			Success: false,
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	_, err := p.Search(context.Background(), "test", "all")
	if err == nil {
		t.Fatal("expected error for unsuccessful search response")
	}
}

func TestQobuzGetArtist_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzArtistResponse{
			Success: true,
			Data: QobuzArtistData{
				Artist: QobuzArtistFull{
					ID:   123,
					Name: QobuzNameObject{Display: "Test Artist"},
				},
			},
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	artist, err := p.GetArtist(context.Background(), "123")
	if err != nil {
		t.Fatalf("GetArtist failed: %v", err)
	}
	if artist.Name != "Test Artist" {
		t.Errorf("artist name = %q, want %q", artist.Name, "Test Artist")
	}
	if artist.ID != "123" {
		t.Errorf("artist ID = %q, want %q", artist.ID, "123")
	}
}

func TestQobuzSearch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzSearchResponse{
			Success: true,
			Data: QobuzSearchData{
				Query: "test",
				Albums: QobuzSearchAlbums{
					Items: []QobuzSearchAlbumItem{
						{
							ID:     "album-1",
							Title:  "Test Album",
							Artist: QobuzArtistRef{ID: 1, Name: "Artist"},
							Image:  QobuzImage{Large: "http://example.com/img.jpg"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	result, err := p.Search(context.Background(), "test", "all")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(result.Albums) != 1 {
		t.Fatalf("expected 1 album, got %d", len(result.Albums))
	}
	if result.Albums[0].Title != "Test Album" {
		t.Errorf("album title = %q, want %q", result.Albums[0].Title, "Test Album")
	}
}

func TestQobuzGetAlbum_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzAlbumDataResponse{
			Success: true,
			Data: &QobuzAlbumResponse{
				ID:     "album-uuid",
				Title:  "Test Album",
				Artist: QobuzArtistRef{ID: 1, Name: "Artist"},
				Image:  QobuzImage{Large: "http://example.com/img.jpg"},
			},
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	album, err := p.GetAlbum(context.Background(), "album-uuid")
	if err != nil {
		t.Fatalf("GetAlbum failed: %v", err)
	}
	if album.Title != "Test Album" {
		t.Errorf("album title = %q, want %q", album.Title, "Test Album")
	}
}

func TestQobuzGetAlbum_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzAlbumDataResponse{
			Success: false,
			Data:    nil,
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	_, err := p.GetAlbum(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for album not found")
	}
}

func TestQobuzGetTrack_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzTrackDataResponse{
			Success: false,
			Data:    nil,
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	_, err := p.GetTrack(context.Background(), "99999")
	if err == nil {
		t.Fatal("expected error for track not found")
	}
}

func TestQobuzResolveTrackID_EmptyISRC(t *testing.T) {
	p := &QobuzProvider{}

	tid, err := p.resolveTrackID(context.Background(), "42", "")
	if err != nil {
		t.Fatalf("resolveTrackID failed: %v", err)
	}
	if tid != 42 {
		t.Errorf("resolveTrackID = %d, want 42", tid)
	}

	_, err = p.resolveTrackID(context.Background(), "", "")
	if err == nil {
		t.Fatal("expected error when both empty")
	}
}

func TestQobuzGetStream_DownloadResponseFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzDownloadResponse{
			Success: false,
			Data:    nil,
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	_, _, err := p.GetStream(context.Background(), "1", "", "LOSSLESS")
	if err == nil {
		t.Fatal("expected error for unsuccessful download response")
	}
}

func TestQobuzGetStream_MissingStreamURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzDownloadResponse{
			Success: true,
			Data:    &QobuzDownloadData{URL: ""},
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	_, _, err := p.GetStream(context.Background(), "1", "", "LOSSLESS")
	if err == nil {
		t.Fatal("expected error for missing stream URL")
	}
}

func TestQobuzResolveTrackID_SearchFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/get-music" {
			json.NewEncoder(w).Encode(QobuzSearchResponse{
				Success: true,
				Data: QobuzSearchData{
					Query: "UYB282636622",
					Tracks: QobuzSearchTracks{
						Items: []QobuzTrackItem{
							{ID: 416711618, Title: "EL MUNDO X TI", ISRC: "UYB282636622", Performer: QobuzPerformer{ID: 1, Name: "Rels B"}},
						},
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	t.Run("fallback to search when get-track fails", func(t *testing.T) {
		p := NewQobuzProvider(srv.URL)

		tid, err := p.resolveTrackID(context.Background(), "", "UYB282636622")
		if err != nil {
			t.Fatalf("resolveTrackID failed: %v", err)
		}
		if tid != 416711618 {
			t.Errorf("resolveTrackID = %d, want 416711618", tid)
		}
	})

	t.Run("search with empty results falls back to numeric", func(t *testing.T) {
		srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/get-music" {
				json.NewEncoder(w).Encode(QobuzSearchResponse{
					Success: true,
					Data: QobuzSearchData{
						Query:  "NOTFOUND",
						Tracks: QobuzSearchTracks{Items: nil},
					},
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("not found"))
		}))
		defer srv2.Close()

		p := NewQobuzProvider(srv2.URL)

		tid, err := p.resolveTrackID(context.Background(), "42", "NOTFOUND")
		if err != nil {
			t.Fatalf("resolveTrackID failed: %v", err)
		}
		if tid != 42 {
			t.Errorf("resolveTrackID = %d, want 42", tid)
		}
	})

	t.Run("search non-200 falls back to numeric", func(t *testing.T) {
		srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("503"))
		}))
		defer srv3.Close()

		p := NewQobuzProvider(srv3.URL)

		tid, err := p.resolveTrackID(context.Background(), "99", "ANYISRC")
		if err != nil {
			t.Fatalf("resolveTrackID failed: %v", err)
		}
		if tid != 99 {
			t.Errorf("resolveTrackID = %d, want 99", tid)
		}
	})
}

func TestQobuzSearch_FiltersByType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzSearchResponse{
			Success: true,
			Data: QobuzSearchData{
				Query: "test",
				Albums: QobuzSearchAlbums{
					Items: []QobuzSearchAlbumItem{
						{ID: "a1", Title: "Album", Artist: QobuzArtistRef{ID: 1, Name: "A"}, Image: QobuzImage{Large: "http://img.com/a.jpg"}},
					},
				},
				Tracks: QobuzSearchTracks{
					Items: []QobuzTrackItem{
						{
							ID: 101, Title: "Track",
							Performer: QobuzPerformer{ID: 1, Name: "Artist"},
						},
					},
				},
				Artists: QobuzSearchArtists{
					Items: []QobuzSearchArtistItem{
						{ID: 1, Name: "Artist"},
					},
				},
				Playlists: QobuzSearchPlaylists{
					Items: []QobuzSearchPlaylistItem{
						{ID: 1, Title: "Playlist"},
					},
				},
			},
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	t.Run("filter by album", func(t *testing.T) {
		result, err := p.Search(context.Background(), "test", "album")
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Albums) == 0 {
			t.Error("expected albums when filtering by album")
		}
		if len(result.Tracks) != 0 {
			t.Error("expected no tracks when filtering by album")
		}
		if len(result.Artists) != 0 {
			t.Error("expected no artists when filtering by album")
		}
	})

	t.Run("filter by track", func(t *testing.T) {
		result, err := p.Search(context.Background(), "test", "track")
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Tracks) == 0 {
			t.Error("expected tracks when filtering by track")
		}
		if len(result.Albums) != 0 {
			t.Error("expected no albums when filtering by track")
		}
	})
}

func TestQobuzGet_Non200Status_ReturnsClearError(t *testing.T) {
	t.Run("404 HTML response produces status error, not JSON parse error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("<html><body>Not Found</body></html>"))
		}))
		defer srv.Close()

		p := NewQobuzProvider(srv.URL)
		_, err := p.GetTrack(context.Background(), "1")
		if err == nil {
			t.Fatal("expected error for 404 response")
		}
		if errMsg := err.Error(); !contains(errMsg, "404") {
			t.Errorf("expected error to mention status 404, got: %s", errMsg)
		}
	})

	t.Run("401 JSON response produces status error with body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
		}))
		defer srv.Close()

		p := NewQobuzProvider(srv.URL)
		_, err := p.GetTrack(context.Background(), "1")
		if err == nil {
			t.Fatal("expected error for 401 response")
		}
		if errMsg := err.Error(); !contains(errMsg, "401") {
			t.Errorf("expected error to mention status 401, got: %s", errMsg)
		}
	})

	t.Run("502 HTML response from ISRC lookup produces status error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("<html>502 Bad Gateway</html>"))
		}))
		defer srv.Close()

		p := NewQobuzProvider(srv.URL)
		_, _, err := p.GetStream(context.Background(), "1", "USABC1234567", "LOSSLESS")
		if err == nil {
			t.Fatal("expected error for 502 response")
		}
		if errMsg := err.Error(); !contains(errMsg, "502") {
			t.Errorf("expected error to mention status 502, got: %s", errMsg)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestQobuzGetTrack_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(QobuzTrackDataResponse{
			Success: true,
			Data: &QobuzTrackResponse{
				ID:        555,
				Title:     "Test Track",
				ISRC:      "USABC1234567",
				Performer: QobuzPerformer{ID: 1, Name: "Artist"},
			},
		})
	}))
	defer srv.Close()

	p := NewQobuzProvider(srv.URL)

	track, err := p.GetTrack(context.Background(), "555")
	if err != nil {
		t.Fatalf("GetTrack failed: %v", err)
	}
	if track.Title != "Test Track" {
		t.Errorf("track title = %q, want %q", track.Title, "Test Track")
	}
	if track.ID != "555" {
		t.Errorf("track ID = %q, want %q", track.ID, "555")
	}
}
