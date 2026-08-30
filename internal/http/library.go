package httpapp

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cesargomez89/navidrums/internal/app"
	"github.com/cesargomez89/navidrums/internal/domain"
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

// DownloadMissingHTMX queues only the tracks of an album that the library does
// not already hold.
//
// "Download Full Album" re-fetches everything, which for a library of
// cherry-picked singles means repeatedly downloading tracks that are already
// there. This queues the gap instead.
func (h *Handler) DownloadMissingHTMX(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	album, err := h.ProviderManager.Provider().GetAlbum(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	index := h.libraryIndex()
	if index == nil {
		// Without a library index there is no "missing" to compute, and
		// silently queuing everything would be a nasty surprise.
		writeAlert(w, "alert-error", "No music library is configured, so missing tracks cannot be determined.")
		return
	}

	if _, err := index.OwnershipFor(album.Tracks); err != nil {
		h.Logger.Error("Ownership lookup failed", "album", id, "error", err)
		writeAlert(w, "alert-error", "Could not check the library: "+err.Error())
		return
	}

	queued, failed := 0, 0
	for i := range album.Tracks {
		if album.Tracks[i].Owned {
			continue
		}
		if _, err := h.JobService.EnqueueJob(album.Tracks[i].ID, domain.JobTypeTrack); err != nil {
			h.Logger.Error("Failed to queue missing track",
				"album", id, "track", album.Tracks[i].ID, "error", err)
			failed++
			continue
		}
		queued++
	}

	switch {
	case queued == 0 && failed == 0:
		writeAlert(w, "alert-success", "You already have every track on this album.")
	case failed > 0:
		writeAlert(w, "alert-error",
			fmt.Sprintf("Queued %d track(s); %d could not be queued.", queued, failed))
	default:
		writeAlert(w, "alert-success", fmt.Sprintf("Queued %d missing track(s).", queued))
	}
}

func writeAlert(w http.ResponseWriter, class, message string) {
	_, _ = fmt.Fprintf(w, "<div class='alert %s'>%s</div>", class, html.EscapeString(message))
}

// enqueueAlbumTracks queues an album, skipping whatever the library already
// holds.
//
// Queueing the whole album re-fetches tracks that are already there, which on a
// library of cherry-picked singles is how duplicates get made. Skipping them is
// the useful default; forceAll exists for deliberately re-downloading, say to
// replace lossy copies.
func (h *Handler) enqueueAlbumTracks(r *http.Request, albumID string, forceAll bool) (queued, skipped int, err error) {
	album, err := h.ProviderManager.Provider().GetAlbum(r.Context(), albumID)
	if err != nil {
		return 0, 0, err
	}
	if len(album.Tracks) == 0 {
		return 0, 0, fmt.Errorf("this album has no tracks to download")
	}

	index := h.libraryIndex()
	if index != nil && !forceAll {
		if _, ownErr := index.OwnershipFor(album.Tracks); ownErr != nil {
			// A failed lookup must not silently turn this into a full
			// re-download; fall back to queueing everything and say nothing
			// was skipped.
			h.Logger.Error("Ownership lookup failed", "album", albumID, "error", ownErr)
		}
	}

	for i := range album.Tracks {
		if !forceAll && album.Tracks[i].Owned {
			skipped++
			continue
		}
		if _, jobErr := h.JobService.EnqueueJob(album.Tracks[i].ID, domain.JobTypeTrack); jobErr != nil {
			h.Logger.Error("Could not queue a track",
				"album", albumID, "track", album.Tracks[i].ID, "error", jobErr)
			continue
		}
		queued++
	}

	return queued, skipped, nil
}

// trackAlreadyHeld reports whether the library already holds this track, and
// why, phrased for the person about to download it.
//
// A lossy copy is not treated as "held": upgrading it is a legitimate reason to
// download, and blocking that would make the feature obstructive rather than
// useful.
func (h *Handler) trackAlreadyHeld(r *http.Request, trackID string) (bool, string) {
	index := h.libraryIndex()
	if index == nil {
		return false, ""
	}

	track, err := h.ProviderManager.Provider().GetTrack(r.Context(), trackID)
	if err != nil || track == nil {
		// Never block a download because the lookup failed.
		return false, ""
	}

	// OwnershipFor marks the slice elements in place, so read the result back
	// from the slice rather than from the original pointer.
	marked := []domain.CatalogTrack{*track}
	if _, err := index.OwnershipFor(marked); err != nil {
		h.Logger.Error("Ownership check failed", "track", trackID, "error", err)
		return false, ""
	}

	if marked[0].Owned && marked[0].OwnedLossless {
		return true, fmt.Sprintf("%q is already in your library as a lossless copy.", track.Title)
	}
	return false, ""
}
