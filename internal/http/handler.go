package httpapp

import (
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/form/v4"

	"github.com/cesargomez89/navidrums/internal/app"
	"github.com/cesargomez89/navidrums/internal/catalog"
	"github.com/cesargomez89/navidrums/internal/config"
	"github.com/cesargomez89/navidrums/internal/logger"
	"github.com/cesargomez89/navidrums/internal/store"
	"github.com/cesargomez89/navidrums/web"
)

type Handler struct {
	cachedRecsTime   time.Time
	JobService       *app.JobService
	DownloadsService *app.DownloadsService
	ProviderManager  *catalog.ProviderManager
	SettingsRepo     *store.SettingsRepo
	DB               *store.DB
	Config           *config.Config
	Templates        *template.Template
	Logger           *logger.Logger
	FormDecoder      *form.Decoder
	cachedRecs       *RecommendationsData
	recsMutex        sync.RWMutex
}

func NewHandler(js *app.JobService, ds *app.DownloadsService, pm *catalog.ProviderManager, sr *store.SettingsRepo, db *store.DB, cfg *config.Config) *Handler {
	h := &Handler{
		JobService:       js,
		DownloadsService: ds,
		ProviderManager:  pm,
		SettingsRepo:     sr,
		DB:               db,
		Config:           cfg,
		Logger:           logger.Default(),
		FormDecoder:      form.NewDecoder(),
	}
	h.ParseTemplates()
	return h
}

func (h *Handler) ParseTemplates() {
	// Not used globally anymore
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.SearchPage)
	r.Get("/htmx/search", h.SearchHTMX)
	r.Get("/htmx/lucky", h.LuckyHTMX)
	r.Get("/artist/{id}", h.ArtistPage)
	r.Get("/album/{id}", h.AlbumPage)
	r.Get("/htmx/album/{id}/similar", h.SimilarAlbumsHTMX)
	r.Get("/htmx/artist/{id}/similar", h.SimilarArtistsHTMX)
	r.Get("/playlist/{id}", h.PlaylistPage)

	r.Get("/htmx/discover/row/{kind}", h.DiscoverRowHTMX)
	r.Get("/htmx/genre-picker", h.GenrePickerHTMX)
	r.Get("/genre/{id}", h.GenrePage)
	r.Get("/label/{id}", h.LabelPage)

	r.Post("/htmx/download/{type}/{id}", h.DownloadHTMX)
	r.Get("/queue", h.QueuePage)
	r.Get("/htmx/queue/active", h.QueueActiveHTMX)
	r.Get("/htmx/queue/history", h.QueueHistoryHTMX)
	r.Post("/htmx/cancel/{id}", h.CancelJobHTMX)
	r.Post("/htmx/retry/{id}", h.RetryJobHTMX)
	r.Post("/htmx/history/clear", h.ClearHistoryHTMX)

	r.Get("/downloads", h.DownloadsPage)
	r.Get("/htmx/downloads", h.DownloadsHTMX)
	r.Post("/htmx/downloads/sync", h.SyncAllHTMX)
	r.Post("/htmx/downloads/bulk-delete", h.BulkDeleteHTMX)
	r.Post("/htmx/downloads/bulk-sync", h.BulkSyncHTMX)
	r.Post("/htmx/downloads/enrich-provider", h.BulkEnrichProviderHTMX)
	r.Post("/htmx/downloads/enrich-musicbrainz", h.BulkEnrichMusicBrainzHTMX)
	r.Post("/htmx/downloads/bulk-genre", h.BulkUpdateGenreHTMX)
	r.Delete("/htmx/download/{id}", h.DeleteDownloadHTMX)

	r.Get("/stream/{id}", h.StreamTrack)

	r.Get("/track/{id}", h.TrackPage)
	r.Get("/htmx/track/{id}", h.TrackHTMX)
	r.Post("/htmx/track/{id}/save", h.SaveTrackHTMX)
	r.Post("/htmx/track/{id}/sync", h.SyncTrackHTMX)
	r.Post("/htmx/track/{id}/enrich", h.EnrichTrackHTMX)
	r.Post("/htmx/track/{id}/enrich-provider", h.EnrichProviderHTMX)

	// Settings and everything that mutates them sit behind the admin gate,
	// which is a no-op when NAVIDRUMS_ADMIN_PASSWORD is unset.
	r.Post("/htmx/admin/unlock", h.AdminUnlockHTMX)
	r.Post("/htmx/admin/lock", h.AdminLockHTMX)

	r.Group(func(r chi.Router) {
		r.Use(h.requireAdmin)

		r.Get("/settings", h.SettingsPage)

		r.Get("/htmx/qobuz-credentials", h.GetQobuzCredentialsHTMX)
		r.Post("/htmx/qobuz-credentials", h.SetQobuzCredentialsHTMX)
		r.Get("/htmx/qobuz-status", h.GetQobuzStatusHTMX)

		r.Get("/htmx/notifications", h.GetNotificationsHTMX)
		r.Post("/htmx/notifications", h.SetNotificationsHTMX)
		r.Post("/htmx/notifications/test", h.TestNotificationHTMX)



		r.Get("/htmx/discover-rows", h.GetDiscoverRowsHTMX)
		r.Post("/htmx/discover-rows", h.SetDiscoverRowsHTMX)
		r.Post("/htmx/discover-rows/reset", h.ResetDiscoverRowsHTMX)

		r.Get("/htmx/genre-map", h.GetGenreMapHTMX)
		r.Post("/htmx/genre-map", h.SetGenreMapHTMX)
		r.Post("/htmx/genre-map/reset", h.ResetGenreMapHTMX)

		r.Get("/htmx/mood-list", h.GetMoodListHTMX)
		r.Post("/htmx/mood-list", h.SetMoodListHTMX)
		r.Post("/htmx/mood-list/reset", h.ResetMoodListHTMX)

		r.Get("/htmx/language-list", h.GetLanguageListHTMX)
		r.Post("/htmx/language-list", h.SetLanguageListHTMX)
		r.Post("/htmx/language-list/reset", h.ResetLanguageListHTMX)

		r.Get("/htmx/genre-separator", h.GetGenreSeparatorHTMX)
		r.Post("/htmx/genre-separator", h.SetGenreSeparatorHTMX)

		r.Get("/htmx/theme", h.GetThemeHTMX)
		r.Post("/htmx/theme", h.SetThemeHTMX)
		r.Post("/htmx/theme/reset", h.ResetThemeHTMX)

		r.Get("/htmx/force-download", h.GetForceDownloadHTMX)
		r.Post("/htmx/force-download", h.SetForceDownloadHTMX)

		r.Get("/htmx/quality", h.GetQualityHTMX)
		r.Post("/htmx/quality", h.SetQualityHTMX)
		r.Post("/htmx/quality/reset", h.ResetQualityHTMX)
	})

	r.Get("/version", h.VersionHTMX)
	r.Get("/img", h.ImageProxy)

	r.Get("/htmx/moods", h.GetMoodsHTMX)
	r.Get("/htmx/languages", h.GetLanguagesHTMX)
}

func (h *Handler) RenderPage(w http.ResponseWriter, pageTmpl string, data interface{}) {
	// Register template functions before parsing
	tmpl := template.New("base").Funcs(template.FuncMap{"join": strings.Join, "img": ProxiedImageURL})
	tmpl, err := tmpl.ParseFS(web.Files,
		"templates/base.html",
		"templates/"+pageTmpl,
		"templates/search_results.html",
		"templates/components/*.html",
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Inject global theme if not already set in data
	if m, ok := data.(map[string]interface{}); ok {
		if _, exists := m["Theme"]; !exists {
			theme := ""
			if h.SettingsRepo != nil {
				theme, _ = h.SettingsRepo.Get(store.SettingTheme)
			}
			if theme == "" {
				theme = h.Config.Theme
			}
			m["Theme"] = theme
		}
	}

	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (h *Handler) RenderFragment(w http.ResponseWriter, fragTmpl string, data interface{}) {
	patterns := []string{"templates/components/*.html", "templates/" + fragTmpl}

	// Register functions before parsing
	tmpl := template.New("frag").Funcs(template.FuncMap{"join": strings.Join, "img": ProxiedImageURL})
	tmpl, err := tmpl.ParseFS(web.Files, patterns...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	name := strings.TrimSuffix(filepath.Base(fragTmpl), ".html")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
