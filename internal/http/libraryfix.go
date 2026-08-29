package httpapp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cesargomez89/navidrums/internal/app"
	"github.com/cesargomez89/navidrums/internal/store"
)

// LibraryFixesHTMX renders the dry-run panel: progress while a scan runs, and
// a summary of what a cleanup would change once one has.
func (h *Handler) LibraryFixesHTMX(w http.ResponseWriter, r *http.Request) {
	h.RenderFragment(w, "components/library_fixes.html", h.libraryFixView())
}

// LibraryDryRunHTMX starts a scan and immediately re-renders the panel, which
// then polls for progress. The scan itself is thousands of throttled catalog
// lookups and cannot run inside a request.
func (h *Handler) LibraryDryRunHTMX(w http.ResponseWriter, r *http.Request) {
	if h.LibraryFixes == nil {
		h.RenderFragment(w, "components/library_fixes.html", h.libraryFixView())
		return
	}

	// Deliberately not the request context: that is cancelled the moment this
	// response returns, which would kill the scan immediately. The scan gets
	// its own generous deadline instead.
	full := r.URL.Query().Get("full") == "1"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	if err := h.LibraryFixes.StartDryRun(ctx, full, cancel); err != nil &&
		!errors.Is(err, app.ErrDryRunRunning) {
		cancel()
		h.Logger.Error("Could not start the library dry run", "error", err)
	}

	h.RenderFragment(w, "components/library_fixes.html", h.libraryFixView())
}

func (h *Handler) libraryFixView() map[string]interface{} {
	view := map[string]interface{}{"Available": false}

	if h.LibraryFixes == nil || h.LibraryService == nil || !h.LibraryService.Configured() {
		return view
	}
	view["Available"] = true

	p := h.LibraryFixes.Progress()
	view["Running"] = p.Running
	view["Scanned"] = p.Scanned
	view["Total"] = p.Total
	view["Matched"] = p.Matched
	view["LastError"] = p.LastError
	if p.Total > 0 {
		view["Percent"] = p.Scanned * 100 / p.Total
	}
	if !p.Finished.IsZero() {
		view["Finished"] = p.Finished.Format("2006-01-02 15:04:05")
	}

	if h.DB != nil {
		summary, err := h.DB.SummariseLibraryFixes()
		if err != nil {
			h.Logger.Error("Could not summarise library fixes", "error", err)
		} else if summary.Total > 0 {
			view["Summary"] = summary
			view["Fields"] = fieldRows(summary)
		}
	}

	return view
}

// fieldRows orders the per-field counts so the panel does not reshuffle
// between renders the way ranging over a map would.
func fieldRows(s *store.FixSummary) []map[string]interface{} {
	order := []struct{ key, label string }{
		{app.FieldISRC, "ISRC"},
		{app.FieldGenre, "Genre"},
		{app.FieldYear, "Year"},
		{app.FieldTrackNumber, "Track number"},
		{app.FieldDiscNumber, "Disc number"},
	}

	rows := make([]map[string]interface{}, 0, len(order))
	for _, o := range order {
		if n := s.ByField[o.key]; n > 0 {
			rows = append(rows, map[string]interface{}{"Label": o.label, "Count": n})
		}
	}
	return rows
}
