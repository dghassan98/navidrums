package store

import (
	"time"
)

// Fix kinds and confidences. These decide what may be applied without review:
// only an exact match filling an empty tag is safe to apply unattended.
const (
	FixKindFill   = "fill"   // the library tag is empty
	FixKindChange = "change" // the library tag disagrees with the source

	FixConfidenceExact = "exact" // matched on ISRC
	FixConfidenceFuzzy = "fuzzy" // matched on normalised title and artist

	FixStatusProposed = "proposed"
	FixStatusApproved = "approved"
	FixStatusRejected = "rejected"
	FixStatusApplied  = "applied"
)

// LibraryFix is one proposed change to one tag on one file.
type LibraryFix struct {
	NavidromeID   string `db:"navidrome_id"`
	Field         string `db:"field"`
	CurrentValue  string `db:"current_value"`
	ProposedValue string `db:"proposed_value"`
	Kind          string `db:"kind"`
	Confidence    string `db:"confidence"`
	SourceTrackID string `db:"source_track_id"`
	Status        string `db:"status"`
}

// ReplaceLibraryFixes swaps the whole proposal set for a freshly generated one.
//
// Proposals already acted on are kept: re-running the dry run must not
// resurrect something rejected, nor forget what was applied.
func (db *DB) ReplaceLibraryFixes(fixes []LibraryFix) error {
	tx, err := db.root.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM library_fixes WHERE status = ?`, FixStatusProposed); err != nil {
		return err
	}

	const insert = `
		INSERT OR IGNORE INTO library_fixes (
			navidrome_id, field, current_value, proposed_value,
			kind, confidence, source_track_id, status, created_at
		) VALUES (
			:navidrome_id, :field, :current_value, :proposed_value,
			:kind, :confidence, :source_track_id, :status, :created_at
		)`

	now := time.Now()
	for i := range fixes {
		if _, err := tx.NamedExec(insert, libraryFixRow{
			LibraryFix: fixes[i],
			CreatedAt:  now,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

type libraryFixRow struct {
	LibraryFix
	CreatedAt time.Time `db:"created_at"`
}

// FixSummary counts proposals by the axes that decide how they are handled.
type FixSummary struct {
	ByField      map[string]int
	Total        int
	Fill         int
	Change       int
	Exact        int
	Fuzzy        int
	AutoApplyOK  int
	NeedsReview  int
	FilesTouched int
}

// SummariseLibraryFixes reports what a cleanup would do, without doing it.
func (db *DB) SummariseLibraryFixes() (*FixSummary, error) {
	out := &FixSummary{ByField: map[string]int{}}

	rows, err := db.Queryx(`
		SELECT field, kind, confidence, COUNT(*) AS n
		FROM library_fixes WHERE status = ?
		GROUP BY field, kind, confidence`, FixStatusProposed)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var field, kind, confidence string
		var n int
		if err := rows.Scan(&field, &kind, &confidence, &n); err != nil {
			return nil, err
		}

		out.Total += n
		out.ByField[field] += n

		if kind == FixKindFill {
			out.Fill += n
		} else {
			out.Change += n
		}
		if confidence == FixConfidenceExact {
			out.Exact += n
		} else {
			out.Fuzzy += n
		}

		// Only an exact match filling an empty tag qualifies for unattended
		// application. Anything fuzzy, or anything overwriting an existing
		// value, is reviewed.
		if kind == FixKindFill && confidence == FixConfidenceExact {
			out.AutoApplyOK += n
		} else {
			out.NeedsReview += n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := db.QueryRow(
		`SELECT COUNT(DISTINCT navidrome_id) FROM library_fixes WHERE status = ?`,
		FixStatusProposed).Scan(&out.FilesTouched); err != nil {
		return nil, err
	}

	return out, nil
}

// ProposedFixesFor returns every proposed change for one file.
func (db *DB) ProposedFixesFor(navidromeID string) ([]LibraryFix, error) {
	var fixes []LibraryFix
	err := db.Select(&fixes, `
		SELECT navidrome_id, field, COALESCE(current_value,'') AS current_value,
		       proposed_value, kind, confidence,
		       COALESCE(source_track_id,'') AS source_track_id, status
		FROM library_fixes WHERE navidrome_id = ? ORDER BY field`, navidromeID)
	return fixes, err
}

// LibraryTracksForFix streams the index so a dry run can walk it without
// holding the whole library in memory twice.
func (db *DB) LibraryTracksForFix() ([]LibraryTrack, error) {
	var tracks []LibraryTrack
	err := db.Select(&tracks, `
		SELECT navidrome_id, COALESCE(isrc,'') AS isrc,
		       title_key, artist_key, COALESCE(artist_primary_key,'') AS artist_primary_key,
		       COALESCE(album_key,'') AS album_key,
		       COALESCE(title,'') AS title, COALESCE(artist,'') AS artist,
		       COALESCE(album,'') AS album, COALESCE(genre,'') AS genre,
		       COALESCE(year,0) AS year, COALESCE(duration,0) AS duration,
		       COALESCE(track_number,0) AS track_number,
		       COALESCE(disc_number,0) AS disc_number,
		       COALESCE(suffix,'') AS suffix, COALESCE(bit_rate,0) AS bit_rate,
		       COALESCE(bit_depth,0) AS bit_depth, lossless,
		       COALESCE(path,'') AS path
		FROM library_tracks ORDER BY navidrome_id`)
	return tracks, err
}
