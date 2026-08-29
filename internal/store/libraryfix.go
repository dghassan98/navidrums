package store

import (
	"errors"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
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

// FixFile groups every proposed change for one file, which is how they are
// reviewed: a file's fields are judged together, not one at a time.
type FixFile struct {
	NavidromeID string
	Title       string
	Artist      string
	Album       string
	Path        string
	Fixes       []LibraryFix
}

// FixFilter narrows the review queue.
type FixFilter struct {
	Kind       string // "", FixKindFill, FixKindChange
	Confidence string // "", FixConfidenceExact, FixConfidenceFuzzy
	Field      string // "", a field name
}

func (f FixFilter) where() (string, []interface{}) {
	clauses := []string{"f.status = ?"}
	args := []interface{}{FixStatusProposed}

	if f.Kind != "" {
		clauses = append(clauses, "f.kind = ?")
		args = append(args, f.Kind)
	}
	if f.Confidence != "" {
		clauses = append(clauses, "f.confidence = ?")
		args = append(args, f.Confidence)
	}
	if f.Field != "" {
		clauses = append(clauses, "f.field = ?")
		args = append(args, f.Field)
	}

	return strings.Join(clauses, " AND "), args
}

// CountFilesAwaitingReview reports how many files still have proposals.
func (db *DB) CountFilesAwaitingReview(filter FixFilter) (int, error) {
	where, args := filter.where()

	var n int
	err := db.QueryRow(
		`SELECT COUNT(DISTINCT f.navidrome_id) FROM library_fixes f WHERE `+where,
		args...).Scan(&n)
	return n, err
}

// FilesAwaitingReview returns one page of files, each with all its proposals.
//
// Ordered by navidrome_id so paging is stable: ordering by anything the review
// itself changes would make rows shuffle between pages as you work.
func (db *DB) FilesAwaitingReview(filter FixFilter, limit, offset int) ([]FixFile, error) {
	where, args := filter.where()

	var ids []string
	idQuery := `SELECT DISTINCT f.navidrome_id FROM library_fixes f WHERE ` + where +
		` ORDER BY f.navidrome_id LIMIT ? OFFSET ?`
	if err := db.Select(&ids, idQuery, append(args, limit, offset)...); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	query, inArgs, err := sqlx.In(`
		SELECT f.navidrome_id, f.field, COALESCE(f.current_value,'') AS current_value,
		       f.proposed_value, f.kind, f.confidence,
		       COALESCE(f.source_track_id,'') AS source_track_id, f.status,
		       COALESCE(lt.title,'') AS title, COALESCE(lt.artist,'') AS artist,
		       COALESCE(lt.album,'') AS album, COALESCE(lt.path,'') AS path
		FROM library_fixes f
		LEFT JOIN library_tracks lt ON lt.navidrome_id = f.navidrome_id
		WHERE f.status = ? AND f.navidrome_id IN (?)
		ORDER BY f.navidrome_id, f.field`, FixStatusProposed, ids)
	if err != nil {
		return nil, err
	}

	rows, err := db.Queryx(db.Rebind(query), inArgs...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	byID := make(map[string]*FixFile, len(ids))
	for rows.Next() {
		var fix LibraryFix
		var title, artist, album, path string
		if err := rows.Scan(&fix.NavidromeID, &fix.Field, &fix.CurrentValue,
			&fix.ProposedValue, &fix.Kind, &fix.Confidence,
			&fix.SourceTrackID, &fix.Status,
			&title, &artist, &album, &path); err != nil {
			return nil, err
		}

		file, ok := byID[fix.NavidromeID]
		if !ok {
			file = &FixFile{
				NavidromeID: fix.NavidromeID,
				Title:       title, Artist: artist, Album: album, Path: path,
			}
			byID[fix.NavidromeID] = file
		}
		file.Fixes = append(file.Fixes, fix)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Return in the id order the page was selected in, not map order.
	files := make([]FixFile, 0, len(ids))
	for _, id := range ids {
		if f, ok := byID[id]; ok {
			files = append(files, *f)
		}
	}
	return files, nil
}

// SetFixStatus marks one file's proposals. Passing fields restricts it to
// those; empty means every proposal on the file.
func (db *DB) SetFixStatus(navidromeID, status string, fields []string) error {
	if len(fields) == 0 {
		_, err := db.Exec(
			`UPDATE library_fixes SET status = ? WHERE navidrome_id = ? AND status = ?`,
			status, navidromeID, FixStatusProposed)
		return err
	}

	query, args, err := sqlx.In(
		`UPDATE library_fixes SET status = ?
		 WHERE navidrome_id = ? AND status = ? AND field IN (?)`,
		status, navidromeID, FixStatusProposed, fields)
	if err != nil {
		return err
	}
	_, err = db.Exec(db.Rebind(query), args...)
	return err
}

// UpdateFixValue replaces a proposed value with one entered by hand.
//
// An empty value is refused rather than stored: writing an empty tag is the
// one thing the cleanup must never do.
func (db *DB) UpdateFixValue(navidromeID, field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("a proposed value cannot be empty")
	}

	_, err := db.Exec(
		`UPDATE library_fixes SET proposed_value = ?
		 WHERE navidrome_id = ? AND field = ? AND status = ?`,
		value, navidromeID, field, FixStatusProposed)
	return err
}

// AlwaysReviewFields never apply unattended, whatever the match confidence.
// Title is here because a rip artefact and an owner's own annotation look
// identical to any rule.
var AlwaysReviewFields = []string{"title"}

// ApproveSafeFixes approves every proposal eligible to apply unattended: an
// exact ISRC match filling an empty tag. Nothing fuzzy, nothing overwriting,
// and never a field that always needs a person.
func (db *DB) ApproveSafeFixes() (int64, error) {
	res, err := db.Exec(
		`UPDATE library_fixes SET status = ?
		 WHERE status = ? AND kind = ? AND confidence = ? AND field NOT IN ('title')`,
		FixStatusApproved, FixStatusProposed, FixKindFill, FixConfidenceExact)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// FixStatusCounts reports the review queue by status.
func (db *DB) FixStatusCounts() (map[string]int, error) {
	counts := map[string]int{}

	rows, err := db.Queryx(`SELECT status, COUNT(*) FROM library_fixes GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		counts[status] = n
	}
	return counts, rows.Err()
}

// UnscannedLibraryTracks returns tracks that have never been examined for
// fixes — in practice, whatever has been downloaded since the last scan.
func (db *DB) UnscannedLibraryTracks() ([]LibraryTrack, error) {
	var tracks []LibraryTrack
	err := db.Select(&tracks, `
		SELECT lt.navidrome_id, COALESCE(lt.isrc,'') AS isrc,
		       lt.title_key, lt.artist_key,
		       COALESCE(lt.artist_primary_key,'') AS artist_primary_key,
		       COALESCE(lt.album_key,'') AS album_key,
		       COALESCE(lt.title,'') AS title, COALESCE(lt.artist,'') AS artist,
		       COALESCE(lt.album,'') AS album, COALESCE(lt.genre,'') AS genre,
		       COALESCE(lt.year,0) AS year, COALESCE(lt.duration,0) AS duration,
		       COALESCE(lt.track_number,0) AS track_number,
		       COALESCE(lt.disc_number,0) AS disc_number,
		       COALESCE(lt.suffix,'') AS suffix, COALESCE(lt.bit_rate,0) AS bit_rate,
		       COALESCE(lt.bit_depth,0) AS bit_depth, lt.lossless,
		       COALESCE(lt.path,'') AS path
		FROM library_tracks lt
		LEFT JOIN library_scan_state s ON s.navidrome_id = lt.navidrome_id
		WHERE s.navidrome_id IS NULL
		ORDER BY lt.navidrome_id`)
	return tracks, err
}

// MarkScanned records that these tracks have been examined.
func (db *DB) MarkScanned(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := db.root.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, id := range ids {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO library_scan_state (navidrome_id, scanned_at)
			 VALUES (?, CURRENT_TIMESTAMP)`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ClearScanState forgets what has been scanned, so the next run is a full one.
func (db *DB) ClearScanState() error {
	_, err := db.Exec(`DELETE FROM library_scan_state`)
	return err
}

// AppendLibraryFixes adds proposals without disturbing existing ones.
//
// An incremental run must not touch what is already in the queue: replacing
// the set wholesale would discard proposals for every track it did not look at.
func (db *DB) AppendLibraryFixes(fixes []LibraryFix) error {
	if len(fixes) == 0 {
		return nil
	}

	tx, err := db.root.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

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
			LibraryFix: fixes[i], CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ApprovedFixFiles returns files with approved changes, ready to apply.
func (db *DB) ApprovedFixFiles(limit int) ([]FixFile, error) {
	var ids []string
	if err := db.Select(&ids, `
		SELECT DISTINCT navidrome_id FROM library_fixes
		WHERE status = ? ORDER BY navidrome_id LIMIT ?`,
		FixStatusApproved, limit); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	query, args, err := sqlx.In(`
		SELECT f.navidrome_id, f.field, COALESCE(f.current_value,'') AS current_value,
		       f.proposed_value, f.kind, f.confidence,
		       COALESCE(f.source_track_id,'') AS source_track_id, f.status,
		       COALESCE(lt.title,'') AS title, COALESCE(lt.artist,'') AS artist,
		       COALESCE(lt.album,'') AS album, COALESCE(lt.path,'') AS path
		FROM library_fixes f
		LEFT JOIN library_tracks lt ON lt.navidrome_id = f.navidrome_id
		WHERE f.status = ? AND f.navidrome_id IN (?)
		ORDER BY f.navidrome_id, f.field`, FixStatusApproved, ids)
	if err != nil {
		return nil, err
	}

	rows, err := db.Queryx(db.Rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	byID := map[string]*FixFile{}
	for rows.Next() {
		var fix LibraryFix
		var title, artist, album, path string
		if err := rows.Scan(&fix.NavidromeID, &fix.Field, &fix.CurrentValue,
			&fix.ProposedValue, &fix.Kind, &fix.Confidence, &fix.SourceTrackID,
			&fix.Status, &title, &artist, &album, &path); err != nil {
			return nil, err
		}
		file, ok := byID[fix.NavidromeID]
		if !ok {
			file = &FixFile{NavidromeID: fix.NavidromeID,
				Title: title, Artist: artist, Album: album, Path: path}
			byID[fix.NavidromeID] = file
		}
		file.Fixes = append(file.Fixes, fix)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]FixFile, 0, len(ids))
	for _, id := range ids {
		if f, ok := byID[id]; ok {
			out = append(out, *f)
		}
	}
	return out, nil
}

// RecordApplied stores what was replaced and marks the fixes done, in one
// transaction: a backup that is not written must not leave a change recorded as
// applied, or the undo would be incomplete.
func (db *DB) RecordApplied(navidromeID, path string, previous, applied map[string]string) error {
	tx, err := db.root.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for field, value := range applied {
		if _, err := tx.Exec(`
			INSERT INTO library_fix_backups
				(navidrome_id, path, field, previous_value, applied_value)
			VALUES (?, ?, ?, ?, ?)`,
			navidromeID, path, field, previous[field], value); err != nil {
			return err
		}

		if _, err := tx.Exec(`
			UPDATE library_fixes SET status = ?
			WHERE navidrome_id = ? AND field = ? AND status = ?`,
			FixStatusApplied, navidromeID, field, FixStatusApproved); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CountApprovedFixes reports how much is waiting to be applied.
func (db *DB) CountApprovedFixes() (files int, fixes int, err error) {
	if err = db.QueryRow(
		`SELECT COUNT(DISTINCT navidrome_id), COUNT(*) FROM library_fixes WHERE status = ?`,
		FixStatusApproved).Scan(&files, &fixes); err != nil {
		return 0, 0, err
	}
	return files, fixes, nil
}
