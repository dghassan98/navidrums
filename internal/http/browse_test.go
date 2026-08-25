package httpapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/cesargomez89/navidrums/internal/catalog"
	"github.com/cesargomez89/navidrums/internal/config"
	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/logger"
)

// failingProvider embeds the mock and overrides one call to fail, so a test
// can prove a broken row degrades instead of taking the page down.
type failingProvider struct {
	*catalog.MockProvider
	err error
}

func (f *failingProvider) GetFeatured(ctx context.Context, kind, genreID string, limit, offset int) ([]domain.Album, error) {
	return nil, f.err
}

func (f *failingProvider) GetGenres(ctx context.Context) ([]domain.Genre, error) {
	return nil, f.err
}

func newBrowseHandler(t *testing.T) *Handler {
	t.Helper()

	return &Handler{
		Config: &config.Config{},
		Logger: logger.New(logger.Config{Level: "error", Format: "text"}),
	}
}

// serveRoute runs one chi route so URL parameters resolve the way they do in
// production.
func serveRoute(h *Handler, method, pattern, target string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	switch pattern {
	case "/htmx/discover/row/{kind}":
		r.Get(pattern, h.DiscoverRowHTMX)
	case "/genre/{id}":
		r.Get(pattern, h.GenrePage)
	case "/label/{id}":
		r.Get(pattern, h.LabelPage)
	}

	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestDiscoverRowRejectsUnknownKind(t *testing.T) {
	h := newBrowseHandler(t)

	rec := serveRoute(h, http.MethodGet, "/htmx/discover/row/{kind}", "/htmx/discover/row/bogus-type")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: an unknown row must not error the page", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Unknown row type") {
		t.Errorf("body did not explain the unknown row: %s", rec.Body.String())
	}
}

func TestDiscoverRowRendersQuietNoteOnProviderFailure(t *testing.T) {
	// One dead row must not take down the whole home page.
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(
		&failingProvider{MockProvider: catalog.NewMockProvider(), err: errors.New("qobuz is away")})

	rec := serveRoute(h, http.MethodGet, "/htmx/discover/row/{kind}", "/htmx/discover/row/new-releases")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "New Releases") {
		t.Error("the row heading should still render")
	}
	if !strings.Contains(body, "qobuz is away") {
		t.Errorf("the failure was not surfaced: %s", body)
	}
}

func TestDiscoverRowRendersAlbums(t *testing.T) {
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(catalog.NewMockProvider())

	rec := serveRoute(h, http.MethodGet, "/htmx/discover/row/{kind}", "/htmx/discover/row/new-releases")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Featured Mock Album 1") {
		t.Errorf("albums did not render: %s", body)
	}
	if !strings.Contains(body, "New Releases") {
		t.Error("row title did not render")
	}
}

func TestDiscoverRowRendersPlaylists(t *testing.T) {
	// The playlists row is not a getFeatured type and takes a different path.
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(catalog.NewMockProvider())

	rec := serveRoute(h, http.MethodGet, "/htmx/discover/row/{kind}", "/htmx/discover/row/playlists")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Featured Mock Playlist") {
		t.Errorf("playlists did not render: %s", rec.Body.String())
	}
}

func TestGenrePageRendersWhenGenreLookupFails(t *testing.T) {
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(
		&failingProvider{MockProvider: catalog.NewMockProvider(), err: errors.New("qobuz is away")})

	rec := serveRoute(h, http.MethodGet, "/genre/{id}", "/genre/112")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even without the genre tree", rec.Code)
	}
}

func TestGenrePageShowsSubgenres(t *testing.T) {
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(catalog.NewMockProvider())

	rec := serveRoute(h, http.MethodGet, "/genre/{id}", "/genre/112")

	body := rec.Body.String()
	if !strings.Contains(body, "Pop/Rock") {
		t.Errorf("genre name did not render: %s", body)
	}
	if !strings.Contains(body, "/genre/117") {
		t.Error("sub-genre links did not render")
	}
}

func TestGenrePageFindsNestedGenre(t *testing.T) {
	// Selecting a child should offer its siblings, not dead end.
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(catalog.NewMockProvider())

	rec := serveRoute(h, http.MethodGet, "/genre/{id}", "/genre/117")

	body := rec.Body.String()
	if !strings.Contains(body, "/genre/119") {
		t.Errorf("sibling sub-genres were not offered: %s", body)
	}
}

func TestLabelPageRendersAlbums(t *testing.T) {
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(catalog.NewMockProvider())

	rec := serveRoute(h, http.MethodGet, "/label/{id}", "/label/1246569")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Mock Label") {
		t.Errorf("label name did not render: %s", body)
	}
	if !strings.Contains(body, "Label Mock Album 1") {
		t.Error("label albums did not render")
	}
}

func TestLabelPageSortDefaultsToNewest(t *testing.T) {
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(catalog.NewMockProvider())

	for _, target := range []string{"/label/1?sort=", "/label/1?sort=sideways", "/label/1"} {
		rec := serveRoute(h, http.MethodGet, "/label/{id}", target)
		if !strings.Contains(rec.Body.String(), "sort=newest") {
			t.Errorf("%s did not fall back to newest-first", target)
		}
	}
}

func TestSortAlbumsOrdersByYear(t *testing.T) {
	albums := []domain.Album{{Year: 1999}, {Year: 2020}, {Year: 2005}}

	sortAlbums(albums, "newest")
	if albums[0].Year != 2020 || albums[2].Year != 1999 {
		t.Errorf("newest-first gave %d, %d, %d", albums[0].Year, albums[1].Year, albums[2].Year)
	}

	sortAlbums(albums, "oldest")
	if albums[0].Year != 1999 || albums[2].Year != 2020 {
		t.Errorf("oldest-first gave %d, %d, %d", albums[0].Year, albums[1].Year, albums[2].Year)
	}
}

func TestSortAlbumsPutsUnknownYearsLast(t *testing.T) {
	// An unknown year is not "year zero" — those albums must not lead the
	// list just because their date is missing.
	albums := []domain.Album{{Year: 0, Title: "unknown"}, {Year: 2020}, {Year: 1999}}

	sortAlbums(albums, "newest")
	if albums[2].Title != "unknown" {
		t.Errorf("newest-first put the undated album at %d, want last", indexOfTitle(albums, "unknown"))
	}

	sortAlbums(albums, "oldest")
	if albums[2].Title != "unknown" {
		t.Errorf("oldest-first put the undated album at %d, want last", indexOfTitle(albums, "unknown"))
	}
}

func TestSortAlbumsIsStableWithinAYear(t *testing.T) {
	albums := []domain.Album{
		{Year: 2020, Title: "first"},
		{Year: 2020, Title: "second"},
		{Year: 2020, Title: "third"},
	}

	sortAlbums(albums, "newest")

	if albums[0].Title != "first" || albums[1].Title != "second" || albums[2].Title != "third" {
		t.Errorf("albums sharing a year were reordered: %v", []string{albums[0].Title, albums[1].Title, albums[2].Title})
	}
}

func TestAlbumSortOrderNormalises(t *testing.T) {
	for _, raw := range []string{"", "sideways", "NEWEST", "desc"} {
		if got := albumSortOrder(raw); got != "newest" {
			t.Errorf("albumSortOrder(%q) = %q, want newest", raw, got)
		}
	}
	if got := albumSortOrder("oldest"); got != "oldest" {
		t.Errorf("albumSortOrder(oldest) = %q", got)
	}
}

func indexOfTitle(albums []domain.Album, title string) int {
	for i := range albums {
		if albums[i].Title == title {
			return i
		}
	}
	return -1
}

func TestDiscoverRowDoesNotServeTheForYouRow(t *testing.T) {
	// for-you is a valid configurable row, but the page renders it inline
	// against /htmx/lucky. Reaching it here would send "for-you" to Qobuz as
	// a featured type, which is a 400.
	h := newBrowseHandler(t)
	h.ProviderManager = catalog.NewProviderManagerWithProvider(catalog.NewMockProvider())

	rec := serveRoute(h, http.MethodGet, "/htmx/discover/row/{kind}", "/htmx/discover/row/for-you")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "htmx/lucky") {
		t.Errorf("body should say where the row comes from: %s", rec.Body.String())
	}
}
