// Package constants contains application-wide constants to avoid magic numbers and strings.
package constants

import "time"

// Application defaults
const (
	DefaultPort                = "8080"
	DefaultDBPath              = "navidrums.db"
	DefaultQuality             = "LOSSLESS"
	DefaultConcurrency         = 2
	DefaultPollInterval        = 2 * time.Second
	DefaultHTTPTimeout         = 1 * time.Minute
	ImageHTTPTimeout           = 30 * time.Second
	DefaultRetryCount          = 8
	DefaultRetryBase           = 1 * time.Second
	DefaultUsername            = "navidrums"
	DefaultSubdirTemplate      = "{{.AlbumArtist}}/{{.OriginalYear}} - {{.Album}}/{{.Disc}}-{{.Track}} {{.Title}}"
	DefaultCacheTTL            = 12 * time.Hour
	DefaultMusicBrainzCacheTTL = 7 * 24 * time.Hour
)

// Quality levels
const (
	QualityLossless      = "LOSSLESS"
	QualityHiResLossless = "HI_RES_LOSSLESS"
	QualityHigh          = "HIGH"
	QualityLow           = "LOW"
)

// Catalog provider defaults
const (
	// QobuzDirectDefaultURL is the official Qobuz API, used when no explicit
	// endpoint is configured.
	QobuzDirectDefaultURL = "https://www.qobuz.com/api.json/0.2"
)

// MIME Types
const (
	MimeTypeFLAC = "audio/flac"
	MimeTypeMP3  = "audio/mpeg"
	MimeTypeMP4  = "audio/mp4"
	MimeTypeJPEG = "image/jpeg"
)

// Database
const (
	JobsTable      = "jobs"
	DownloadsTable = "downloads"
	CacheTable     = "cache"
)

// File Extensions
const (
	ExtFLAC = ".flac"
	ExtMP3  = ".mp3"
	ExtMP4  = ".mp4"
	ExtM4A  = ".m4a"
	ExtM3U  = ".m3u"
	ExtJPG  = ".jpg"
)

// File Names
const (
	PlaylistsDir  = "playlists"
	CoverFileName = "cover.jpg"
)

// File Permissions
const (
	DirPermissions  = 0750
	FilePermissions = 0600
)

// HTTP Status Codes
const (
	StatusOK                 = 200
	StatusBadRequest         = 400
	StatusNotFound           = 404
	StatusInternalError      = 500
	StatusServiceUnavailable = 503
)

// UI/UX
const (
	MaxHistoryItems     = 20
	MaxSearchResults    = 30
	ProgressUpdateFreq  = 2 * time.Second
	ProgressUpdateBytes = 1024 * 1024 // 1MB
)

// Characters to sanitize from filesystem paths
const InvalidPathChars = "<>:\"/\\|?*"
