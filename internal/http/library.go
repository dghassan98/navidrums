package httpapp

import (
	"context"
	"net/http"
	"time"

	"github.com/cesargomez89/navidrums/internal/app"
)

// LibraryStatusHTMX renders the read-only library index panel in Settings.
func (h *Handler) LibraryStatusHTMX(w http.ResponseWriter, r *http.Request) {
	h.RenderFragment(w, "components/library_status.html", h.libraryStatusView())
}

// LibrarySyncHTMX rebuilds the index, then re-renders the panel.
//
// The sync runs inline rather than as a queued job: it is a handful of API
// calls over a few seconds, and doing it inline means the panel shows the real
// outcome instead of "queued".
func (h *Handler) LibrarySyncHTMX(w http.ResponseWriter, r *http.Request) {
	if h.LibraryService == nil || !h.LibraryService.Configured() {
		h.RenderFragment(w, "components/library_status.html", h.libraryStatusView())
		return
	}

	ctx, cancel := contextWithTimeout(r, 5*time.Minute)
	defer cancel()

	if err := h.LibraryService.Sync(ctx); err != nil {
		h.Logger.Error("Library sync failed", "error", err)
	}

	h.RenderFragment(w, "components/library_status.html", h.libraryStatusView())
}

func (h *Handler) libraryStatusView() map[string]interface{} {
	view := map[string]interface{}{
		"Configured": false,
	}
	if h.LibraryService == nil {
		return view
	}

	status := h.LibraryService.Status()
	view["Configured"] = status.Configured
	view["Syncing"] = status.Syncing
	view["LastError"] = status.LastError

	if !status.LastRun.IsZero() {
		view["LastRun"] = status.LastRun.Format("2006-01-02 15:04:05")
	}

	if status.Stats != nil {
		view["Tracks"] = status.Stats.Tracks
		view["WithISRC"] = status.Stats.WithISRC
		view["Lossless"] = status.Stats.Lossless
		view["Albums"] = status.Stats.DistinctAlbs
		if status.Stats.Tracks > 0 {
			view["ISRCPercent"] = percent(status.Stats.WithISRC, status.Stats.Tracks)
			view["LosslessPercent"] = percent(status.Stats.Lossless, status.Stats.Tracks)
		}
		if status.Stats.LastSynced != nil {
			view["LastSynced"] = status.Stats.LastSynced.Format("2006-01-02 15:04:05")
		}
	}

	return view
}

func percent(part, total int) int {
	if total == 0 {
		return 0
	}
	return part * 100 / total
}

// libraryIndex returns the ownership source, or nil when no library is
// configured. Callers treat nil as "no marks", never as an error.
func (h *Handler) libraryIndex() app.LibraryIndex {
	if h.LibraryService == nil || !h.LibraryService.Configured() {
		return nil
	}
	return h.LibraryService
}

// contextWithTimeout bounds a long-running handler without inheriting the
// request's own deadline semantics.
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
