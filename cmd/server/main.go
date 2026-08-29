package main

import (
	"context"
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"golang.org/x/time/rate"

	"github.com/cesargomez89/navidrums/internal/app"
	"github.com/cesargomez89/navidrums/internal/catalog"
	"github.com/cesargomez89/navidrums/internal/config"
	"github.com/cesargomez89/navidrums/internal/downloader"
	httpapp "github.com/cesargomez89/navidrums/internal/http"
	"github.com/cesargomez89/navidrums/internal/logger"
	"github.com/cesargomez89/navidrums/internal/store"
	"github.com/cesargomez89/navidrums/internal/subsonic"
	"github.com/cesargomez89/navidrums/web"
)

func main() {
	cfg := config.Load()

	// Initialize Logger
	appLogger := logger.New(logger.Config{
		Level:  cfg.LogLevel,
		Format: cfg.LogFormat,
	})

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		appLogger.Error("Configuration error", "error", err)
		os.Exit(1)
	}

	// Initialize DB
	db, err := store.NewSQLiteDB(cfg.DBPath)
	if err != nil {
		appLogger.Error("Failed to init DB", "error", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			appLogger.Error("Failed to close DB", "error", closeErr)
		}
	}()

	// Initialize Settings Repo (needed by ProviderManager)
	settingsRepo := store.NewSettingsRepo(db)

	// Initialize Provider Manager (no system default — providers configured via UI)
	providerManager := catalog.NewProviderManager(db, settingsRepo, cfg.CacheTTL, appLogger)
	providerManager.SetQobuzCredentials(catalog.QobuzCredentials{
		AppID:       cfg.QobuzAppID,
		AppSecret:   cfg.QobuzAppSecret,
		Email:       cfg.QobuzEmail,
		PasswordMD5: cfg.QobuzPasswordMD5,
		AuthToken:   cfg.QobuzAuthToken,
	})

	// Initialize Worker
	w := downloader.NewWorker(db, settingsRepo, providerManager, cfg, appLogger)
	w.Start()
	defer w.Stop()

	// Initialize Services
	jobService := app.NewJobService(db, appLogger)
	downloadsService := app.NewDownloadsService(db, appLogger)

	// Read-only index of the existing music library. Optional: without a
	// Navidrome connection the browse pages simply show no ownership marks.
	libraryService := app.NewLibraryService(
		subsonic.NewClient(cfg.NavidromeURL, cfg.NavidromeUser, cfg.NavidromePassword),
		db, appLogger.Logger)

	// Applies approved fixes to the files. Refuses unless deliberately
	// enabled through the environment.
	libraryApplyService := app.NewLibraryApplyService(
		db, cfg.LibraryMount, cfg.LibrarySourcePrefix, cfg.LibraryWrite, appLogger.Logger)

	// Dry-run cleanup: compares the library index against the catalog and
	// records what it would change. It writes to no files.
	libraryFixService := app.NewLibraryFixService(
		func() app.CatalogSearcher { return providerManager.Provider() },
		db, appLogger.Logger)

	// Initialize Router
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Basic Auth Middleware (skip if SKIP_AUTH is set)
	if cfg.Password != "" && !cfg.SkipAuth {
		r.Use(basicAuthMiddleware(cfg.Username, cfg.Password))
	}

	// Rate Limiting Middleware (skip if DISABLE_RATE_LIMIT is set, useful when behind Cloudflare)
	if !cfg.DisableRateLimit {
		r.Use(rateLimitMiddleware(cfg.RateLimitRequests, cfg.RateLimitWindow, cfg.RateLimitBurst))
	}

	// Serve Static Files from embedded filesystem
	r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := "static" + r.URL.Path[len("/static"):]
		data, err := web.Files.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		contentType := "application/octet-stream"
		switch {
		case strings.HasSuffix(path, ".css"):
			contentType = "text/css"
		case strings.HasSuffix(path, ".js"):
			contentType = "application/javascript"
		case strings.HasSuffix(path, ".ico"):
			contentType = "image/x-icon"
		case strings.HasSuffix(path, ".png"):
			contentType = "image/png"
		case strings.HasSuffix(path, ".jpg"), strings.HasSuffix(path, ".jpeg"):
			contentType = "image/jpeg"
		case strings.HasSuffix(path, ".svg"):
			contentType = "image/svg+xml"
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(data)
	}))

	r.Get("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/favicon.ico", http.StatusMovedPermanently)
	})

	// Routes
	h := httpapp.NewHandler(jobService, downloadsService, providerManager, settingsRepo, db, libraryService, libraryFixService, libraryApplyService, cfg)
	h.RegisterRoutes(r)

	// Start Server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		appLogger.Info("Server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("Server error", "error", err)
		}
	}()

	// Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLogger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		appLogger.Error("Server forced to shutdown", "error", err)
	}

	appLogger.Info("Server exiting")
}

func basicAuthMiddleware(username, password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Navidrums"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func (i *ipLimiter) Allow() bool {
	return i.limiter.Allow()
}

func rateLimitMiddleware(requestsPerWindow int, window time.Duration, burst int) func(http.Handler) http.Handler {
	limiters := &sync.Map{}
	cleanupInterval := 5 * time.Minute

	go func() {
		ticker := time.NewTicker(cleanupInterval)
		for range ticker.C {
			now := time.Now()
			limiters.Range(func(key, value any) bool {
				ip := key.(string)
				limiter := value.(*ipLimiter)
				if now.Sub(limiter.lastSeen) > window*2 {
					limiters.Delete(ip)
				}
				return true
			})
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Static assets and artwork are cheap, cacheable and inherently
			// bursty: one page of results asks for dozens of covers at once.
			// Counting them starves the requests that actually matter.
			if isRateLimitExempt(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			ip := getIP(r)
			l, exists := limiters.Load(ip)
			if !exists {
				l = &ipLimiter{
					limiter:  rate.NewLimiter(rate.Limit(float64(requestsPerWindow)/window.Seconds()), burst),
					lastSeen: time.Now(),
				}
				limiters.Store(ip, l)
			}
			limiter := l.(*ipLimiter)
			limiter.lastSeen = time.Now()

			if !limiter.Allow() {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// isRateLimitExempt reports paths that should never be throttled.
func isRateLimitExempt(path string) bool {
	return strings.HasPrefix(path, "/static/") || path == "/img"
}

func getIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// RemoteAddr carries host:port, and the port differs on every connection.
	// Returning it whole gave each request its own limiter, so a client that
	// was not behind a proxy was never rate limited at all.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
