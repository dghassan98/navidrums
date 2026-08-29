package httpapp

import (
	"net/http"
	"strconv"

	"github.com/cesargomez89/navidrums/internal/app"
)

// LibraryApplyStatusHTMX renders the apply panel.
func (h *Handler) LibraryApplyStatusHTMX(w http.ResponseWriter, r *http.Request) {
	h.RenderFragment(w, "components/library_apply.html", h.applyView(nil))
}

// LibraryApplyHTMX applies approved changes, or reports what it would do.
//
// The batch is deliberately small by default: the agreed sequence is to try a
// handful of files and check them in the music server before letting it loose
// on the rest.
func (h *Handler) LibraryApplyHTMX(w http.ResponseWriter, r *http.Request) {
	if h.LibraryApply == nil {
		h.RenderFragment(w, "components/library_apply.html", h.applyView(nil))
		return
	}

	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	dryRun := r.URL.Query().Get("dry") != "0"

	report, err := h.LibraryApply.Apply(limit, dryRun)
	if err != nil {
		h.Logger.Error("Library apply failed", "error", err)
		view := h.applyView(nil)
		view["Error"] = err.Error()
		h.RenderFragment(w, "components/library_apply.html", view)
		return
	}

	// A rescan only matters when files actually changed, and only after a real
	// run: without it the music server keeps serving the old tags and the work
	// looks like it did nothing.
	if !dryRun && report.Changed > 0 && h.LibraryService != nil {
		if err := h.LibraryService.TriggerRescan(r.Context()); err != nil {
			h.Logger.Error("Could not trigger a library rescan", "error", err)
			report.RescanError = err.Error()
		} else {
			report.Rescanned = true
		}
	}

	h.RenderFragment(w, "components/library_apply.html", h.applyView(report))
}

func (h *Handler) applyView(report *app.ApplyReport) map[string]interface{} {
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

	if report != nil {
		view["Report"] = report
	}
	return view
}
