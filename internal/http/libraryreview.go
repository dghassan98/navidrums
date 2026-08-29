package httpapp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cesargomez89/navidrums/internal/app"
	"github.com/cesargomez89/navidrums/internal/store"
)

// reviewPageSize is how many files one page of the queue shows. Each file
// carries several proposed fields, so a larger page becomes unreadable rather
// than more efficient.
const reviewPageSize = 25

// LibraryReviewPage renders the review queue.
func (h *Handler) LibraryReviewPage(w http.ResponseWriter, r *http.Request) {
	h.RenderPage(w, "library_review.html", map[string]interface{}{
		"ActivePage": "settings",
		"Filter":     readFixFilter(r),
	})
}

// LibraryReviewListHTMX renders one page of the queue.
func (h *Handler) LibraryReviewListHTMX(w http.ResponseWriter, r *http.Request) {
	h.RenderFragment(w, "components/review_list.html", h.reviewListView(r))
}

func (h *Handler) reviewListView(r *http.Request) map[string]interface{} {
	view := map[string]interface{}{}
	if h.DB == nil {
		return view
	}

	filter := readFixFilter(r)
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			page = n
		}
	}

	total, err := h.DB.CountFilesAwaitingReview(filter)
	if err != nil {
		h.Logger.Error("Could not count the review queue", "error", err)
		return view
	}

	files, err := h.DB.FilesAwaitingReview(filter, reviewPageSize, (page-1)*reviewPageSize)
	if err != nil {
		h.Logger.Error("Could not read the review queue", "error", err)
		return view
	}

	totalPages := (total + reviewPageSize - 1) / reviewPageSize

	view["Files"] = files
	view["Total"] = total
	view["Page"] = page
	view["TotalPages"] = totalPages
	view["HasPrev"] = page > 1
	view["HasNext"] = page < totalPages
	view["PrevPage"] = page - 1
	view["NextPage"] = page + 1
	view["Filter"] = filter
	view["Query"] = filterQuery(filter)

	if counts, err := h.DB.FixStatusCounts(); err == nil {
		view["Counts"] = counts
	}

	return view
}

// LibraryReviewDecideHTMX approves or rejects one file's proposals, applying
// any hand-edited values first.
//
// Approving records intent only. Nothing is written to a file here — applying
// is a separate step that does not exist yet.
func (h *Handler) LibraryReviewDecideHTMX(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	decision := chi.URLParam(r, "decision")

	status := store.FixStatusRejected
	if decision == "approve" {
		status = store.FixStatusApproved
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	// Only fields the reviewer actually ticked are acted on; an unticked field
	// stays proposed rather than being silently swept along with the file.
	fields := r.Form["field"]

	if status == store.FixStatusApproved {
		for _, field := range fields {
			edited := strings.TrimSpace(r.FormValue("value_" + field))
			if edited == "" {
				continue
			}
			if err := h.DB.UpdateFixValue(id, field, edited); err != nil {
				h.Logger.Error("Could not save an edited value",
					"file", id, "field", field, "error", err)
			}
		}
	}

	if err := h.DB.SetFixStatus(id, status, fields); err != nil {
		h.Logger.Error("Could not record a review decision", "file", id, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.RenderFragment(w, "components/review_list.html", h.reviewListView(r))
}

// LibraryReviewApproveSafeHTMX approves everything eligible to apply
// unattended: exact ISRC matches filling an empty tag.
func (h *Handler) LibraryReviewApproveSafeHTMX(w http.ResponseWriter, r *http.Request) {
	n, err := h.DB.ApproveSafeFixes()
	if err != nil {
		h.Logger.Error("Could not approve the safe fixes", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Logger.Info("Approved unattended-safe fixes", "count", n)

	h.RenderFragment(w, "components/review_list.html", h.reviewListView(r))
}

func readFixFilter(r *http.Request) store.FixFilter {
	return store.FixFilter{
		Kind:       validOneOf(r.URL.Query().Get("kind"), store.FixKindFill, store.FixKindChange),
		Confidence: validOneOf(r.URL.Query().Get("confidence"), store.FixConfidenceExact, store.FixConfidenceFuzzy),
		Field: validOneOf(r.URL.Query().Get("field"),
			app.FieldTitle, app.FieldISRC, app.FieldGenre, app.FieldYear,
			app.FieldTrackNumber, app.FieldDiscNumber),
	}
}

// validOneOf keeps an unexpected query value out of the SQL entirely rather
// than trusting it to the parameter binding alone.
func validOneOf(value string, allowed ...string) string {
	for _, a := range allowed {
		if value == a {
			return value
		}
	}
	return ""
}

func filterQuery(f store.FixFilter) string {
	parts := []string{}
	if f.Kind != "" {
		parts = append(parts, "kind="+f.Kind)
	}
	if f.Confidence != "" {
		parts = append(parts, "confidence="+f.Confidence)
	}
	if f.Field != "" {
		parts = append(parts, "field="+f.Field)
	}
	if len(parts) == 0 {
		return ""
	}
	return "&" + strings.Join(parts, "&")
}
