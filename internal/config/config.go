package config

import (
	"crypto/md5" //nolint:gosec // Qobuz specifies MD5 for its password hash
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/cesargomez89/navidrums/internal/constants"
)

// Config holds all application configuration
type Config struct {
	Port                  string
	DBPath                string
	DownloadsDir          string
	Quality               string
	PlayQuality           string
	LogLevel              string
	LogFormat             string
	Username              string
	Password              string
	SubdirTemplate        string
	MusicBrainzURL        string
	FFmpegPath            string
	FFprobePath           string
	Theme                 string
	CacheTTL              time.Duration
	MusicBrainzCacheTTL   time.Duration
	RateLimitWindow       time.Duration
	RateLimitRequests     int
	RateLimitBurst        int
	SkipAuth              bool
	DisableRateLimit      bool
	LyricsFallbackEnabled bool
	LyricsFallbackURL     string
	QobuzAppID            string
	QobuzAppSecret        string
	QobuzEmail            string
	QobuzPasswordMD5      string
	QobuzAuthToken        string
	NavidromeURL          string
	NavidromeUser         string
	NavidromePassword     string
	AdminPassword         string
	NotifyURL             string
	AdminSessionTTL       time.Duration
}

// Load loads configuration from environment variables with defaults
func Load() *Config {
	home, _ := os.UserHomeDir()
	defaultDownload := filepath.Join(home, "Downloads/navidrums")

	return &Config{
		Port:                  getEnv("PORT", constants.DefaultPort),
		DBPath:                getEnv("DB_PATH", constants.DefaultDBPath),
		DownloadsDir:          getEnv("DOWNLOADS_DIR", defaultDownload),
		Quality:               getEnv("QUALITY", constants.DefaultQuality),
		PlayQuality:           getEnv("PLAY_QUALITY", "HIGH"),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		LogFormat:             getEnv("LOG_FORMAT", "text"),
		Username:              getEnv("NAVIDRUMS_USERNAME", constants.DefaultUsername),
		Password:              getEnv("NAVIDRUMS_PASSWORD", ""),
		SubdirTemplate:        getEnv("SUBDIR_TEMPLATE", constants.DefaultSubdirTemplate),
		CacheTTL:              getEnvDuration("CACHE_TTL", constants.DefaultCacheTTL),
		MusicBrainzCacheTTL:   getEnvDuration("MUSICBRAINZ_CACHE_TTL", constants.DefaultMusicBrainzCacheTTL),
		MusicBrainzURL:        getEnv("MUSICBRAINZ_URL", "https://musicbrainz.org/ws/2"),
		RateLimitRequests:     getEnvInt("RATE_LIMIT_REQUESTS", 600),
		RateLimitWindow:       getEnvDuration("RATE_LIMIT_WINDOW", time.Minute),
		RateLimitBurst:        getEnvInt("RATE_LIMIT_BURST", 60),
		SkipAuth:              getEnvBool("SKIP_AUTH", false),
		DisableRateLimit:      getEnvBool("DISABLE_RATE_LIMIT", false),
		Theme:                 getEnv("THEME", "golden"),
		FFmpegPath:            getEnv("FFMPEG_PATH", ""),
		FFprobePath:           getEnv("FFPROBE_PATH", ""),
		LyricsFallbackEnabled: getEnvBool("LYRICS_FALLBACK_ENABLED", true),
		LyricsFallbackURL:     getEnv("LYRICS_FALLBACK_URL", "https://lrclib.net/api/get"),
		QobuzAppID:            getEnv("QOBUZ_APP_ID", ""),
		QobuzAppSecret:        getEnv("QOBUZ_APP_SECRET", ""),
		QobuzEmail:            getEnv("QOBUZ_EMAIL", ""),
		QobuzPasswordMD5:      qobuzPasswordMD5(),
		QobuzAuthToken:        getEnv("QOBUZ_AUTH_TOKEN", ""),
		NavidromeURL:          getEnv("NAVIDROME_URL", ""),
		NavidromeUser:         getEnv("NAVIDROME_USER", ""),
		NavidromePassword:     getEnv("NAVIDROME_PASSWORD", ""),
		AdminPassword:         getEnv("NAVIDRUMS_ADMIN_PASSWORD", ""),
		NotifyURL:             getEnv("NOTIFY_URL", ""),
		AdminSessionTTL:       getEnvDuration("NAVIDRUMS_ADMIN_SESSION_TTL", 30*time.Minute),
	}
}

// qobuzPasswordMD5 reads the Qobuz password, hashing a plaintext one so
// QOBUZ_PASSWORD_MD5 and QOBUZ_PASSWORD are interchangeable. A pre-hashed
// value wins, so a password never has to be stored in the clear.
func qobuzPasswordMD5() string {
	if hashed := getEnv("QOBUZ_PASSWORD_MD5", ""); hashed != "" {
		return strings.ToLower(strings.TrimSpace(hashed))
	}
	plain := getEnv("QOBUZ_PASSWORD", "")
	if plain == "" {
		return ""
	}
	sum := md5.Sum([]byte(plain)) //nolint:gosec // required by the Qobuz API
	return hex.EncodeToString(sum[:])
}

// Validate validates the configuration and returns detailed errors.
func (c *Config) Validate() error {
	var errors []string

	// Validate Port
	if c.Port == "" {
		errors = append(errors, "PORT cannot be empty")
	} else {
		port, err := strconv.Atoi(c.Port)
		if err != nil {
			errors = append(errors, fmt.Sprintf("PORT must be a valid number, got: %s", c.Port))
		} else if port < 1 || port > 65535 {
			errors = append(errors, fmt.Sprintf("PORT must be between 1 and 65535, got: %d", port))
		}
	}

	// Validate DBPath
	if c.DBPath == "" {
		errors = append(errors, "DB_PATH cannot be empty")
	}

	// Validate DownloadsDir
	if c.DownloadsDir == "" {
		errors = append(errors, "DOWNLOADS_DIR cannot be empty")
	}

	// Validate Quality
	validQualities := map[string]bool{
		constants.QualityLossless:      true,
		constants.QualityHiResLossless: true,
		constants.QualityHigh:          true,
		constants.QualityLow:           true,
	}
	if !validQualities[c.Quality] {
		errors = append(errors, fmt.Sprintf("QUALITY must be one of: %s, %s, %s, %s, got: %s",
			constants.QualityLossless, constants.QualityHiResLossless, constants.QualityHigh, constants.QualityLow, c.Quality))
	}

	// Validate LogLevel
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.LogLevel] {
		errors = append(errors, fmt.Sprintf("LOG_LEVEL must be one of: debug, info, warn, error, got: %s", c.LogLevel))
	}

	// Validate LogFormat
	validLogFormats := map[string]bool{
		"text": true,
		"json": true,
	}
	if !validLogFormats[c.LogFormat] {
		errors = append(errors, fmt.Sprintf("LOG_FORMAT must be one of: text, json, got: %s", c.LogFormat))
	}

	// Validate Username (optional - only required if password is set)
	if c.Password != "" && c.Username == "" {
		errors = append(errors, "USERNAME cannot be empty when PASSWORD is set")
	}

	// Password is optional - empty password disables basic auth

	// Validate SubdirTemplate
	if c.SubdirTemplate == "" {
		errors = append(errors, "SUBDIR_TEMPLATE cannot be empty")
	} else {
		if _, err := template.New("subdir").Parse(c.SubdirTemplate); err != nil {
			errors = append(errors, fmt.Sprintf("SUBDIR_TEMPLATE is invalid: %v", err))
		}
	}

	// Validate CacheTTL
	if c.CacheTTL <= 0 {
		errors = append(errors, "CACHE_TTL must be greater than 0")
	}

	// Validate MusicBrainzCacheTTL
	if c.MusicBrainzCacheTTL <= 0 {
		errors = append(errors, "MUSICBRAINZ_CACHE_TTL must be greater than 0")
	}

	// Validate RateLimitRequests
	if c.RateLimitRequests <= 0 {
		errors = append(errors, "RATE_LIMIT_REQUESTS must be greater than 0")
	}

	// Validate RateLimitWindow
	if c.RateLimitWindow <= 0 {
		errors = append(errors, "RATE_LIMIT_WINDOW must be greater than 0")
	}

	// Validate RateLimitBurst
	if c.RateLimitBurst <= 0 {
		errors = append(errors, "RATE_LIMIT_BURST must be greater than 0")
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errors, "\n  - "))
	}

	return nil
}

// getEnv retrieves an environment variable with a fallback default
func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// getEnvInt retrieves an environment variable as int with a fallback default
func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if v, err := strconv.Atoi(value); err == nil {
			return v
		}
	}
	return fallback
}

// getEnvBool retrieves an environment variable as bool with a fallback default
func getEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		return strings.ToLower(value) == "true" || value == "1"
	}
	return fallback
}

// getEnvDuration retrieves an environment variable as time.Duration with a fallback default
func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if value, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return fallback
}
