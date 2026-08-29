package httpapp

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/cesargomez89/navidrums/internal/app"
)

// LibraryApplyStatusHTMX renders the apply panel.
func (h *Handler) LibraryApplyStatusHTMX(w http.ResponseWriter, r *http.Request) {
	h.RenderFragment(w, "components/library_apply.html", h.applyView())
}

// LibraryApplyHTMX applies approved changes, or reports what it would do.
//
// The batch is deliberately small by default: the agreed sequence is to try a
// handful of files and check them in the music server before letting it loose
// on the rest.
func (h *Handler) LibraryApplyHTMX(w http.ResponseWriter, r *http.Request) {
	if h.LibraryApply == nil {
		h.RenderFragment(w, "components/library_apply.html", h.applyView())
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	dryRun := r.URL.Query().Get("dry") != "0"

	// Started in the background and answered immediately: writing tags to
	// twenty files takes far longer than a request should be held open, which
	// is why the first version looked like it had frozen.
	if err := h.LibraryApply.Start(limit, dryRun); err != nil &&
		!errors.Is(err, app.ErrApplyRunning) {
		h.Logger.Error("Could not start the library apply", "error", err)
		view := h.applyView()
		view["Error"] = err.Error()
		h.RenderFragment(w, "components/library_apply.html", view)
		return
	}

	h.RenderFragment(w, "components/library_apply.html", h.applyView())
}

func (h *Handler) applyView() map[string]interface{} {
	view := map[string]interface{}{}

	if h.LibraryApply != nil {
		enabled, mount, reason := h.LibraryApply.Status()
		view["Enabled"] = enabled
		view["Mount"] = mount
		view["Reason"] = reason
	}

	if h.DB != nil {
		if files, fixes, err := h.DB.CountApprovedFixes(); err == nil {
			view["ApprovedFiles"] = files
			view["ApprovedFixes"] = fixes
		}
	}

	if h.LibraryApply != nil {
		progress, report := h.LibraryApply.Progress()
		view["Running"] = progress.Running
		view["Progress"] = progress
		if progress.Total > 0 {
			view["Percent"] = progress.Done * 100 / progress.Total
		}

		// The rescan is triggered once, when a run that changed something has
		// finished — not per file, and never for a dry run.
		if report != nil && !report.DryRun && report.Changed > 0 &&
			!report.Rescanned && report.RescanError == "" && h.LibraryService != nil {
			if err := h.LibraryService.TriggerRescan(context.Background()); err != nil {
				h.Logger.Error("Could not trigger a library rescan", "error", err)
				report.RescanError = err.Error()
			} else {
				report.Rescanned = true
			}
		}

		if report != nil {
			view["Report"] = report
		}
	}
	return view
}
