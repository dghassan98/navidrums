package store

import (
	"time"

	"github.com/jmoiron/sqlx"
)

// LibraryTrack mirrors one song already present in the music library.
//
// This table is a cache of what Navidrome reports and is never authoritative.
// Nothing here is written back: the library is read-only to Navidrums.
type LibraryTrack struct {
	NavidromeID string `db:"navidrome_id"`
	ISRC        string `db:"isrc"`
	TitleKey    string `db:"title_key"`
	ArtistKey   string `db:"artist_key"`
	AlbumKey    string `db:"album_key"`
	Title       string `db:"title"`
	Artist      string `db:"artist"`
	Album       string `db:"album"`
	Suffix      string `db:"suffix"`
	Path        string `db:"path"`
	Year        int    `db:"year"`
	Duration    int    `db:"duration"`
	BitRate     int    `db:"bit_rate"`
	BitDepth    int    `db:"bit_depth"`
	Lossless    bool   `db:"lossless"`
}

// ReplaceLibraryTracks swaps the whole index for a freshly synced one.
//
// Wholesale replacement rather than an upsert-and-prune: the index is
// disposable, a full library is only a few thousand rows, and a transaction
// means a failed sync leaves the previous index intact rather than a partial
// one that would silently under-report what is owned.
func (db *DB) ReplaceLibraryTracks(tracks []LibraryTrack) error {
	tx, err := db.root.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM library_tracks`); err != nil {
		return err
	}

	const insert = `
		INSERT OR REPLACE INTO library_tracks (
			navidrome_id, isrc, title_key, artist_key, album_key,
			title, artist, album, year, duration,
			suffix, bit_rate, bit_depth, lossless, path, synced_at
		) VALUES (
			:navidrome_id, :isrc, :title_key, :artist_key, :album_key,
			:title, :artist, :album, :year, :duration,
			:suffix, :bit_rate, :bit_depth, :lossless, :path, :synced_at
		)`

	now := time.Now()
	for i := range tracks {
		if _, err := tx.NamedExec(insert, libraryTrackRow{
			LibraryTrack: tracks[i],
			SyncedAt:     now,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

type libraryTrackRow struct {
	LibraryTrack
	SyncedAt time.Time `db:"synced_at"`
}

// LibraryStats summarises the index for the settings panel.
type LibraryStats struct {
	LastSynced   *time.Time
	Tracks       int
	WithISRC     int
	Lossless     int
	DistinctAlbs int
}

func (db *DB) LibraryStats() (*LibraryStats, error) {
	stats := &LibraryStats{}

	row := db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN isrc IS NOT NULL AND isrc <> '' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN lossless THEN 1 ELSE 0 END), 0),
		       COUNT(DISTINCT album_key),
		       MAX(synced_at)
		FROM library_tracks`)

	var lastSynced *string
	if err := row.Scan(&stats.Tracks, &stats.WithISRC, &stats.Lossless,
		&stats.DistinctAlbs, &lastSynced); err != nil {
		return nil, err
	}

	if lastSynced != nil {
		if parsed, err := parseSQLiteTime(*lastSynced); err == nil {
			stats.LastSynced = &parsed
		}
	}

	return stats, nil
}

// parseSQLiteTime handles the formats the driver hands back for a DATETIME.
func parseSQLiteTime(raw string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05",
	}
	var err error
	for _, layout := range layouts {
		var parsed time.Time
		if parsed, err = time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, err
}

// LibraryMatch is one library copy of a track, with just enough to judge
// whether it is worth downloading again.
type LibraryMatch struct {
	ISRC      string `db:"isrc"`
	TitleKey  string `db:"title_key"`
	ArtistKey string `db:"artist_key"`
	Lossless  bool   `db:"lossless"`
}

// FindLibraryMatches looks up library copies for a batch of tracks in two
// queries — one keyed on ISRC, one on normalised title and artist — rather
// than one query per track.
func (db *DB) FindLibraryMatches(isrcs []string, titleKeys, artistKeys []string) ([]LibraryMatch, error) {
	matches := make([]LibraryMatch, 0)

	if len(isrcs) > 0 {
		query, args, err := sqlx.In(
			`SELECT isrc, title_key, artist_key, lossless FROM library_tracks
			 WHERE isrc IS NOT NULL AND isrc <> '' AND isrc IN (?)`, isrcs)
		if err != nil {
			return nil, err
		}
		var found []LibraryMatch
		if err := db.Select(&found, db.Rebind(query), args...); err != nil {
			return nil, err
		}
		matches = append(matches, found...)
	}

	if len(titleKeys) > 0 && len(artistKeys) > 0 {
		query, args, err := sqlx.In(
			`SELECT isrc, title_key, artist_key, lossless FROM library_tracks
			 WHERE title_key IN (?) AND artist_key IN (?)`, titleKeys, artistKeys)
		if err != nil {
			return nil, err
		}
		var found []LibraryMatch
		if err := db.Select(&found, db.Rebind(query), args...); err != nil {
			return nil, err
		}
		matches = append(matches, found...)
	}

	return matches, nil
}
