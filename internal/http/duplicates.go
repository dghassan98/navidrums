package httpapp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DuplicatesPage lists groups of files that look like the same recording.
func (h *Handler) DuplicatesPage(w http.ResponseWriter, r *http.Request) {
	h.RenderPage(w, "duplicates.html", map[string]interface{}{
		"ActivePage": "settings",
	})
}

// DuplicatesListHTMX renders the groups.
func (h *Handler) DuplicatesListHTMX(w http.ResponseWriter, r *http.Request) {
	h.RenderFragment(w, "components/duplicates_list.html", h.duplicatesView(""))
}

// DuplicateDeleteHTMX removes one file, chosen explicitly.
//
// One at a time and never in bulk: unlike a tag, a deleted file has no backup
// to restore from, so there is no safe way to offer "delete all the rest".
func (h *Handler) DuplicateDeleteHTMX(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var message string
	if h.Duplicates == nil {
		message = "Duplicate handling is not available."
	} else if err := h.Duplicates.Delete(id); err != nil {
		h.Logger.Error("Could not delete a duplicate", "track", id, "error", err)
		message = err.Error()
	}

	h.RenderFragment(w, "components/duplicates_list.html", h.duplicatesView(message))
}

func (h *Handler) duplicatesView(errorMessage string) map[string]interface{} {
	view := map[string]interface{}{"Error": errorMessage}

	if h.LibraryApply != nil {
		enabled, _, reason := h.LibraryApply.Status()
		view["CanDelete"] = enabled
		view["DeleteReason"] = reason
	}

	if h.Duplicates == nil {
		return view
	}

	groups, err := h.Duplicates.Groups()
	if err != nil {
		h.Logger.Error("Could not read duplicate groups", "error", err)
		view["Error"] = err.Error()
		return view
	}

	files := 0
	uncertain := 0
	for _, g := range groups {
		files += len(g.Copies)
		if g.Uncertain {
			uncertain++
		}
	}

	view["Groups"] = groups
	view["GroupCount"] = len(groups)
	view["FileCount"] = files
	view["UncertainCount"] = uncertain

	return view
}
