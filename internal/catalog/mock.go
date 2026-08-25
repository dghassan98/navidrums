package catalog

import (
	"context"
	"io"
	"strings"

	"github.com/cesargomez89/navidrums/internal/domain"
)

type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (p *MockProvider) Search(ctx context.Context, query string, searchType string) (*domain.SearchResult, error) {
	res := &domain.SearchResult{
		Artists: []domain.Artist{{ID: "1", Name: "Mock Artist"}},
		Albums:  []domain.Album{{ID: "1", Title: "Mock Album", Artist: "Mock Artist"}},
		Tracks:  []domain.CatalogTrack{{ID: "1", Title: "Mock Track", ArtistID: "1", Artist: "Mock Artist", AlbumID: "1", Album: "Mock Album", TrackNumber: 1, Duration: 180}},
	}

	if searchType == "" {
		searchType = "album"
	}

	resFiltered := &domain.SearchResult{}
	switch searchType {
	case "artist":
		resFiltered.Artists = res.Artists
	case "album":
		resFiltered.Albums = res.Albums
	case "track":
		resFiltered.Tracks = res.Tracks
	default:
		resFiltered.Albums = res.Albums
	}
	return resFiltered, nil
}

func (p *MockProvider) GetArtist(ctx context.Context, id string) (*domain.Artist, error) {
	return &domain.Artist{ID: id, Name: "Mock Artist"}, nil
}

func (p *MockProvider) GetAlbum(ctx context.Context, id string) (*domain.Album, error) {
	return &domain.Album{
		ID:       id,
		Title:    "Mock Album",
		Artist:   "Mock Artist",
		ArtistID: "1",
		Tracks: []domain.CatalogTrack{
			{ID: "1", Title: "Track 1", ArtistID: "1", Artist: "Mock Artist", AlbumID: "1", Album: "Mock Album", TrackNumber: 1, Duration: 180},
			{ID: "2", Title: "Track 2", ArtistID: "1", Artist: "Mock Artist", AlbumID: "1", Album: "Mock Album", TrackNumber: 2, Duration: 200},
		},
	}, nil
}

func (p *MockProvider) GetPlaylist(ctx context.Context, id string) (*domain.Playlist, error) {
	return &domain.Playlist{
		ProviderID: id,
		Title:      "Mock Playlist",
		Tracks: []domain.CatalogTrack{
			{ID: "3", Title: "Track 3", ArtistID: "2", Artist: "Unknown", AlbumID: "2", Album: "Unknown Album", TrackNumber: 1},
		},
	}, nil
}

func (p *MockProvider) GetTrack(ctx context.Context, id string) (*domain.CatalogTrack, error) {
	return &domain.CatalogTrack{ID: id, Title: "Mock Track", ArtistID: "1", Artist: "Mock Artist", AlbumID: "1", Album: "Mock Album", TrackNumber: 1}, nil
}

func (p *MockProvider) GetStream(ctx context.Context, trackID string, isrc string, quality string) (io.ReadCloser, string, error) {
	return io.NopCloser(strings.NewReader("dummy audio content")), "audio/flac", nil
}

func (p *MockProvider) GetSimilarAlbums(ctx context.Context, id string) ([]domain.Album, error) {
	return []domain.Album{
		{ID: "101", Title: "Similar Mock Album 1", Artist: "Mock Artist"},
		{ID: "102", Title: "Similar Mock Album 2", Artist: "Mock Artist"},
		{ID: "103", Title: "Similar Mock Album 3", Artist: "Mock Artist"},
		{ID: "104", Title: "Similar Mock Album 4", Artist: "Mock Artist"},
		{ID: "105", Title: "Similar Mock Album 5", Artist: "Mock Artist"},
		{ID: "106", Title: "Similar Mock Album 6", Artist: "Mock Artist"},
		{ID: "107", Title: "Similar Mock Album 7", Artist: "Mock Artist"},
		{ID: "108", Title: "Similar Mock Album 8", Artist: "Mock Artist"},
	}, nil
}

func (p *MockProvider) GetSimilarArtists(ctx context.Context, id string) ([]domain.Artist, error) {
	return []domain.Artist{
		{ID: "201", Name: "Similar Mock Artist 1"},
		{ID: "202", Name: "Similar Mock Artist 2"},
		{ID: "203", Name: "Similar Mock Artist 3"},
		{ID: "204", Name: "Similar Mock Artist 4"},
		{ID: "205", Name: "Similar Mock Artist 5"},
		{ID: "206", Name: "Similar Mock Artist 6"},
		{ID: "207", Name: "Similar Mock Artist 7"},
		{ID: "208", Name: "Similar Mock Artist 8"},
	}, nil
}

func (p *MockProvider) GetLyrics(ctx context.Context, trackID string) (string, string, error) {
	return "Mock lyrics for testing", "[00:00.00] Mock lyrics for testing", nil
}

func (p *MockProvider) GetFeatured(ctx context.Context, kind, genreID string, limit, offset int) ([]domain.Album, error) {
	return []domain.Album{
		{ID: "301", Title: "Featured Mock Album 1", Artist: "Mock Artist", LabelID: "9", GenreID: "112"},
		{ID: "302", Title: "Featured Mock Album 2", Artist: "Mock Artist", LabelID: "9", GenreID: "112"},
	}, nil
}

func (p *MockProvider) GetFeaturedPlaylists(ctx context.Context, genreID string, limit, offset int) ([]domain.Playlist, error) {
	return []domain.Playlist{
		{ProviderID: "401", Title: "Featured Mock Playlist"},
	}, nil
}

func (p *MockProvider) GetGenres(ctx context.Context) ([]domain.Genre, error) {
	return []domain.Genre{
		{ID: "112", Name: "Pop/Rock", Slug: "pop-rock", Children: []domain.Genre{
			{ID: "117", Name: "Pop", Slug: "pop"},
			{ID: "119", Name: "Rock", Slug: "rock"},
		}},
		{ID: "80", Name: "Jazz", Slug: "jazz"},
	}, nil
}

func (p *MockProvider) GetLabel(ctx context.Context, labelID string, limit, offset int) (*domain.Label, error) {
	return &domain.Label{
		ID:          labelID,
		Name:        "Mock Label",
		AlbumsCount: 2,
		Albums: []domain.Album{
			{ID: "501", Title: "Label Mock Album 1", Artist: "Mock Artist"},
			{ID: "502", Title: "Label Mock Album 2", Artist: "Mock Artist"},
		},
	}, nil
}

func (p *MockProvider) GetRecommendations(ctx context.Context, id string) ([]domain.CatalogTrack, error) {
	return []domain.CatalogTrack{
		{ID: "601", Title: "Recommended Mock Track", ArtistID: "1", Artist: "Mock Artist"},
	}, nil
}

var _ Provider = (*MockProvider)(nil)
