package app

import (
	"encoding/json"

	"github.com/cesargomez89/navidrums/internal/catalog"
)

// DiscoverRow is one editorial row on the home page. Kind is either a Qobuz
// album/getFeatured type or PlaylistsRowKind.
type DiscoverRow struct {
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
}

// PlaylistsRowKind is not an album/getFeatured type: it maps to
// playlist/getFeatured and is served by its own route.
const PlaylistsRowKind = "playlists"

// DefaultDiscoverRows is what a fresh install shows. Everything Qobuz offers is
// listed so it can be switched on in Settings, but only a readable handful
// starts enabled.
func DefaultDiscoverRows() []DiscoverRow {
	return []DiscoverRow{
		{Kind: "new-releases", Enabled: true},
		{Kind: "editor-picks", Enabled: true},
		{Kind: "press-awards", Enabled: true},
		{Kind: "ideal-discography", Enabled: true},
		{Kind: PlaylistsRowKind, Enabled: true},
		{Kind: "recent-releases", Enabled: false},
		{Kind: "new-releases-full", Enabled: false},
		{Kind: "most-streamed", Enabled: false},
		{Kind: "most-featured", Enabled: false},
		{Kind: "best-sellers", Enabled: false},
		{Kind: "qobuzissims", Enabled: false},
		{Kind: "harmonia-mundi", Enabled: false},
		{Kind: "universal-classic", Enabled: false},
		{Kind: "universal-jazz", Enabled: false},
		{Kind: "universal-jeunesse", Enabled: false},
		{Kind: "universal-chanson", Enabled: false},
	}
}

// IsValidDiscoverKind reports whether kind is one this app can actually
// request. Qobuz answers an unknown album/getFeatured type with a 400, so an
// invalid stored preference has to be dropped rather than rendered.
func IsValidDiscoverKind(kind string) bool {
	return kind == PlaylistsRowKind || catalog.IsValidFeaturedKind(kind)
}

// ParseDiscoverRows turns stored JSON into the row list, falling back to the
// defaults when the setting is unset or unreadable.
//
// Unknown kinds are dropped and known-but-missing kinds are appended disabled,
// so the list stays complete and valid across upgrades that add or retire a
// Qobuz type without needing a migration.
func ParseDiscoverRows(stored string) []DiscoverRow {
	defaults := DefaultDiscoverRows()
	if stored == "" {
		return defaults
	}

	var parsed []DiscoverRow
	if err := json.Unmarshal([]byte(stored), &parsed); err != nil {
		return defaults
	}

	rows := make([]DiscoverRow, 0, len(defaults))
	seen := make(map[string]bool, len(parsed))
	for _, row := range parsed {
		if !IsValidDiscoverKind(row.Kind) || seen[row.Kind] {
			continue
		}
		seen[row.Kind] = true
		rows = append(rows, row)
	}

	// Nothing stored was usable. Appending the defaults disabled would leave
	// Discover blank with no way to tell why, so treat it as unconfigured.
	if len(rows) == 0 {
		return defaults
	}

	for _, row := range defaults {
		if !seen[row.Kind] {
			rows = append(rows, DiscoverRow{Kind: row.Kind, Enabled: false})
		}
	}

	return rows
}

// EnabledDiscoverRows returns only the rows that should render, in order.
func EnabledDiscoverRows(rows []DiscoverRow) []DiscoverRow {
	enabled := make([]DiscoverRow, 0, len(rows))
	for _, row := range rows {
		if row.Enabled {
			enabled = append(enabled, row)
		}
	}
	return enabled
}

// DiscoverRowTitle is the human label for a row.
func DiscoverRowTitle(kind string) string {
	if title, ok := discoverRowTitles[kind]; ok {
		return title
	}
	return kind
}

var discoverRowTitles = map[string]string{
	"new-releases":       "New Releases",
	"recent-releases":    "Recent Releases",
	"new-releases-full":  "New Releases (Full)",
	"editor-picks":       "Editor Picks",
	"press-awards":       "Press Awards",
	"most-streamed":      "Most Streamed",
	"most-featured":      "Most Featured",
	"best-sellers":       "Best Sellers",
	"ideal-discography":  "Ideal Discography",
	"qobuzissims":        "Qobuzissims",
	"harmonia-mundi":     "Harmonia Mundi",
	"universal-classic":  "Universal Classic",
	"universal-jazz":     "Universal Jazz",
	"universal-jeunesse": "Universal Jeunesse",
	"universal-chanson":  "Universal Chanson",
	PlaylistsRowKind:     "Curated Playlists",
}
