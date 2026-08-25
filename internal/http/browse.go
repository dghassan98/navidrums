package httpapp

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/cesargomez89/navidrums/internal/app"
	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/store"
)

const (
	// discoverRowSize is how many albums a home page row shows. Rows scroll
	// horizontally, so this is about how far you can scroll, not page height.
	discoverRowSize = 20

	// labelPageSize is a full grid page on the label view.
	labelPageSize = 24
)

// DiscoverRowHTMX renders one editorial row. Each row loads itself, so a slow
// or failing row degrades to a quiet note instead of blocking or breaking the
// whole page.
func (h *Handler) DiscoverRowHTMX(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	genreID := r.URL.Query().Get("genre_id")

	if !app.IsValidDiscoverKind(kind) {
		h.renderEmptyRow(w, app.DiscoverRowTitle(kind), "Unknown row type.")
		return
	}

	if kind == app.PlaylistsRowKind {
		h.discoverPlaylistsRow(w, r, genreID)
		return
	}

	albums, err := h.ProviderManager.Provider().GetFeatured(r.Context(), kind, genreID, discoverRowSize, 0)
	if err != nil {
		h.Logger.Error("Discover row failed", "kind", kind, "error", err)
		h.renderEmptyRow(w, app.DiscoverRowTitle(kind), rowErrorMessage(err))
		return
	}

	MarkOwnedAlbums(h.ownershipIndex(), albums)

	h.RenderFragment(w, "components/discover_row.html", map[string]interface{}{
		"Title":  app.DiscoverRowTitle(kind),
		"Albums": albums,
	})
}

func (h *Handler) discoverPlaylistsRow(w http.ResponseWriter, r *http.Request, genreID string) {
	playlists, err := h.ProviderManager.Provider().GetFeaturedPlaylists(r.Context(), genreID, discoverRowSize, 0)
	if err != nil {
		h.Logger.Error("Discover playlists row failed", "error", err)
		h.renderEmptyRow(w, app.DiscoverRowTitle(app.PlaylistsRowKind), rowErrorMessage(err))
		return
	}

	h.RenderFragment(w, "components/discover_row.html", map[string]interface{}{
		"Title":     app.DiscoverRowTitle(app.PlaylistsRowKind),
		"Playlists": playlists,
	})
}

// GenrePickerHTMX renders the genre selector shared by the home and label
// pages.
func (h *Handler) GenrePickerHTMX(w http.ResponseWriter, r *http.Request) {
	genres, err := h.ProviderManager.Provider().GetGenres(r.Context())
	if err != nil {
		h.Logger.Error("Genre list failed", "error", err)
		// A missing picker must not take the page with it.
		genres = nil
	}

	h.RenderFragment(w, "components/genre_picker.html", map[string]interface{}{
		"Genres":    genres,
		"Selected":  r.URL.Query().Get("genre_id"),
		"HXTarget":  r.URL.Query().Get("target"),
		"ActionURL": r.URL.Query().Get("action"),
	})
}

// GenrePage shows one genre: its sibling sub-genres, then the editorial rows
// scoped to it.
func (h *Handler) GenrePage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	genres, err := h.ProviderManager.Provider().GetGenres(r.Context())
	if err != nil {
		h.Logger.Error("Genre list failed", "error", err)
	}

	current, children := findGenre(genres, id)

	h.RenderPage(w, "genre.html", map[string]interface{}{
		"ActivePage": "search",
		"GenreID":    id,
		"Genre":      current,
		"Children":   children,
		"Rows":       h.enabledDiscoverRows(),
	})
}

// findGenre locates a genre anywhere in the two level tree, returning it and
// the children to offer as drill-down. Selecting a child offers its siblings,
// so navigation does not dead end one level down.
func findGenre(genres []domain.Genre, id string) (*domain.Genre, []domain.Genre) {
	for i := range genres {
		if genres[i].ID == id {
			return &genres[i], genres[i].Children
		}
		for j := range genres[i].Children {
			if genres[i].Children[j].ID == id {
				return &genres[i].Children[j], genres[i].Children
			}
		}
	}
	return nil, nil
}

// LabelPage shows one label's catalogue, newest first by default.
func (h *Handler) LabelPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}

	order := albumSortOrder(r.URL.Query().Get("sort"))

	label, err := h.ProviderManager.Provider().GetLabel(
		r.Context(), id, labelPageSize, (page-1)*labelPageSize)
	if err != nil {
		h.Logger.Error("Label page failed", "label", id, "error", err)
		http.Error(w, rowErrorMessage(err), http.StatusBadGateway)
		return
	}

	// Qobuz has no sort parameter here, so ordering is applied to the page it
	// returned. Newest-first therefore orders within the page, not the whole
	// catalogue; that is a real limit, not an oversight.
	sortAlbums(label.Albums, order)
	MarkOwnedAlbums(h.ownershipIndex(), label.Albums)

	totalPages := (label.AlbumsCount + labelPageSize - 1) / labelPageSize

	h.RenderPage(w, "label.html", map[string]interface{}{
		"ActivePage": "search",
		"Label":      label,
		"Sort":       order,
		"Page":       page,
		"TotalPages": totalPages,
		"HasPrev":    page > 1,
		"HasNext":    page < totalPages,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
	})
}

// sortAlbums orders a page of albums by release date. Albums with no year go
// last either way: an unknown date is not "year zero", and burying them keeps
// the useful end of the list at the top whichever direction you pick.
//
// Stable, so albums sharing a year keep the order the provider returned.
func sortAlbums(albums []domain.Album, order string) {
	sort.SliceStable(albums, func(i, j int) bool {
		a, b := albums[i], albums[j]
		switch {
		case a.Year == 0 && b.Year == 0:
			return false
		case a.Year == 0:
			return false
		case b.Year == 0:
			return true
		case order == "oldest":
			return a.Year < b.Year
		default:
			return a.Year > b.Year
		}
	})
}

// albumSortOrder normalises the sort query parameter.
func albumSortOrder(raw string) string {
	if raw == "oldest" {
		return "oldest"
	}
	return "newest"
}

// enabledDiscoverRows reads the configured rows, falling back to the defaults
// whenever the setting is unreadable.
func (h *Handler) enabledDiscoverRows() []app.DiscoverRow {
	return app.EnabledDiscoverRows(app.ParseDiscoverRows(h.storedDiscoverRows()))
}

// storedDiscoverRows reads the setting, treating both an absent repository and
// a read failure as "unset" so the defaults render either way.
func (h *Handler) storedDiscoverRows() string {
	if h.SettingsRepo == nil {
		return ""
	}
	stored, err := h.SettingsRepo.Get(store.SettingDiscoverRows)
	if err != nil {
		return ""
	}
	return stored
}

// MarkOwnedAlbums is a thin alias so page handlers outside this file read the
// same way as the browse ones.
func MarkOwnedAlbums(index app.OwnershipIndex, albums []domain.Album) {
	app.MarkOwned(index, albums)
}

func (h *Handler) ownershipIndex() app.OwnershipIndex {
	if h.DB == nil {
		return nil
	}
	return h.DB
}

func (h *Handler) renderEmptyRow(w http.ResponseWriter, title, message string) {
	h.RenderFragment(w, "components/discover_row.html", map[string]interface{}{
		"Title":   title,
		"Message": message,
	})
}

// rowErrorMessage keeps credential problems legible instead of surfacing a raw
// upstream error, which is the failure people actually hit here.
func rowErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// GetDiscoverRowsHTMX renders the Settings panel listing every row.
func (h *Handler) GetDiscoverRowsHTMX(w http.ResponseWriter, r *http.Request) {
	rows := app.ParseDiscoverRows(h.storedDiscoverRows())
	view := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		view = append(view, map[string]interface{}{
			"Kind":    row.Kind,
			"Title":   app.DiscoverRowTitle(row.Kind),
			"Enabled": row.Enabled,
		})
	}

	h.RenderFragment(w, "components/discover_rows_settings.html", map[string]interface{}{
		"Rows": view,
	})
}

// SetDiscoverRowsHTMX stores the row order and enabled flags.
func (h *Handler) SetDiscoverRowsHTMX(w http.ResponseWriter, r *http.Request) {
	var rows []app.DiscoverRow
	if err := json.NewDecoder(r.Body).Decode(&rows); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Round-trip through the parser so an unknown kind cannot be stored, no
	// matter what the browser posted.
	encoded, err := json.Marshal(rows)
	if err != nil {
		http.Error(w, "Invalid rows", http.StatusBadRequest)
		return
	}
	cleaned, err := json.Marshal(app.ParseDiscoverRows(string(encoded)))
	if err != nil {
		h.Logger.Error("Failed to marshal discover rows", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.SettingsRepo.Set(store.SettingDiscoverRows, string(cleaned)); err != nil {
		h.Logger.Error("Failed to save discover rows", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	_, _ = w.Write([]byte(`{"success":true}`))
}

// ResetDiscoverRowsHTMX restores the defaults and re-renders the panel.
func (h *Handler) ResetDiscoverRowsHTMX(w http.ResponseWriter, r *http.Request) {
	if err := h.SettingsRepo.Delete(store.SettingDiscoverRows); err != nil {
		h.Logger.Error("Failed to reset discover rows", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	h.GetDiscoverRowsHTMX(w, r)
}
