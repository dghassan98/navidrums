package store

import "testing"

func seedFix(t *testing.T, db *DB, id, field, value string) {
	t.Helper()
	if err := db.ReplaceLibraryFixes([]LibraryFix{{
		NavidromeID: id, Field: field, ProposedValue: value,
		Kind: FixKindFill, Confidence: FixConfidenceFuzzy, Status: FixStatusProposed,
	}}); err != nil {
		t.Fatalf("ReplaceLibraryFixes: %v", err)
	}
}

// TestRejectedFixesSurviveARescan is the guarantee behind "leave this file
// alone": a rejection has to outlive the next dry run, or every scan would
// resurrect a change already refused.
func TestRejectedFixesSurviveARescan(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	seedFix(t, db, "n1", "genre", "Pop")
	if err := db.SetFixStatus("n1", FixStatusRejected, nil); err != nil {
		t.Fatalf("SetFixStatus: %v", err)
	}

	// A later dry run proposes the same change again.
	seedFix(t, db, "n1", "genre", "Pop")

	var status string
	if err := db.QueryRow(
		`SELECT status FROM library_fixes WHERE navidrome_id='n1' AND field='genre'`).
		Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != FixStatusRejected {
		t.Errorf("status = %q; a rejected fix must not return as proposed", status)
	}
}

// TestApprovedFixesSurviveARescan covers the other half: a decision already
// made must not be forgotten and re-queued.
func TestApprovedFixesSurviveARescan(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	seedFix(t, db, "n2", "year", "1996")
	if err := db.SetFixStatus("n2", FixStatusApproved, nil); err != nil {
		t.Fatalf("SetFixStatus: %v", err)
	}
	seedFix(t, db, "n2", "year", "1996")

	var status string
	if err := db.QueryRow(
		`SELECT status FROM library_fixes WHERE navidrome_id='n2' AND field='year'`).
		Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != FixStatusApproved {
		t.Errorf("status = %q, want approved", status)
	}
}

func TestUpdateFixValueRefusesEmpty(t *testing.T) {
	// Writing an empty tag is the one thing the cleanup must never do, so a
	// hand-edit cannot blank a value.
	db, cleanup := setupTestDB(t)
	defer cleanup()

	seedFix(t, db, "n3", "genre", "Pop")

	if err := db.UpdateFixValue("n3", "genre", "   "); err == nil {
		t.Error("an empty hand-edited value was accepted")
	}

	var value string
	if err := db.QueryRow(
		`SELECT proposed_value FROM library_fixes WHERE navidrome_id='n3'`).Scan(&value); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if value != "Pop" {
		t.Errorf("value = %q; the original must survive a refused edit", value)
	}
}

func TestApproveSafeFixesOnlyTakesExactFills(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if err := db.ReplaceLibraryFixes([]LibraryFix{
		{NavidromeID: "a", Field: "genre", ProposedValue: "Pop",
			Kind: FixKindFill, Confidence: FixConfidenceExact, Status: FixStatusProposed},
		{NavidromeID: "b", Field: "genre", ProposedValue: "Pop",
			Kind: FixKindFill, Confidence: FixConfidenceFuzzy, Status: FixStatusProposed},
		{NavidromeID: "c", Field: "genre", ProposedValue: "Pop",
			Kind: FixKindChange, Confidence: FixConfidenceExact, Status: FixStatusProposed},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	n, err := db.ApproveSafeFixes()
	if err != nil {
		t.Fatalf("ApproveSafeFixes: %v", err)
	}
	if n != 1 {
		t.Errorf("approved %d, want only the exact fill", n)
	}

	var bad int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM library_fixes WHERE status = ?
		 AND NOT (kind = ? AND confidence = ?)`,
		FixStatusApproved, FixKindFill, FixConfidenceExact).Scan(&bad); err != nil {
		t.Fatalf("check: %v", err)
	}
	if bad != 0 {
		t.Errorf("%d unsafe fixes were approved", bad)
	}
}

// TestIncrementalScanOnlySeesNewTracks is what makes the reviewer usable after
// every download instead of a half-hour event: a track already examined must
// not come back, and a newly downloaded one must.
func TestIncrementalScanOnlySeesNewTracks(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	seedLibrary := func(ids ...string) {
		t.Helper()
		tracks := make([]LibraryTrack, 0, len(ids))
		for _, id := range ids {
			tracks = append(tracks, LibraryTrack{
				NavidromeID: id, Title: id, Artist: "A",
				TitleKey: id, ArtistKey: "a",
			})
		}
		if err := db.ReplaceLibraryTracks(tracks); err != nil {
			t.Fatalf("ReplaceLibraryTracks: %v", err)
		}
	}

	seedLibrary("t1", "t2")

	first, err := db.UnscannedLibraryTracks()
	if err != nil {
		t.Fatalf("UnscannedLibraryTracks: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("got %d unscanned, want 2 on a fresh index", len(first))
	}

	if err := db.MarkScanned([]string{"t1", "t2"}); err != nil {
		t.Fatalf("MarkScanned: %v", err)
	}

	if again, _ := db.UnscannedLibraryTracks(); len(again) != 0 {
		t.Errorf("got %d unscanned after marking, want 0", len(again))
	}

	// A download arrives and the library is re-synced, which rebuilds
	// library_tracks entirely — the scan state must survive that.
	seedLibrary("t1", "t2", "t3")

	next, err := db.UnscannedLibraryTracks()
	if err != nil {
		t.Fatalf("UnscannedLibraryTracks: %v", err)
	}
	if len(next) != 1 || next[0].NavidromeID != "t3" {
		t.Fatalf("got %+v, want only the newly downloaded track", next)
	}

	// A full re-scan deliberately forgets, so everything is examined again.
	if err := db.ClearScanState(); err != nil {
		t.Fatalf("ClearScanState: %v", err)
	}
	if all, _ := db.UnscannedLibraryTracks(); len(all) != 3 {
		t.Errorf("got %d after clearing, want all 3", len(all))
	}
}

// TestAppendKeepsExistingProposals covers the incremental write path: adding
// proposals for new tracks must not discard the queue for everything else.
func TestAppendKeepsExistingProposals(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	seedFix(t, db, "old", "genre", "Pop")

	if err := db.AppendLibraryFixes([]LibraryFix{{
		NavidromeID: "new", Field: "genre", ProposedValue: "Rock",
		Kind: FixKindFill, Confidence: FixConfidenceFuzzy, Status: FixStatusProposed,
	}}); err != nil {
		t.Fatalf("AppendLibraryFixes: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM library_fixes`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("got %d proposals, want both the old and the new", n)
	}
}

// TestApproveSafeFixesNeverTakesATitle is the safety net behind the title
// change: bulk approval must not sweep up a title, even an exact-match fill.
func TestApproveSafeFixesNeverTakesATitle(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	if err := db.ReplaceLibraryFixes([]LibraryFix{
		{NavidromeID: "a", Field: "title", ProposedValue: "One Last Time",
			Kind: FixKindFill, Confidence: FixConfidenceExact, Status: FixStatusProposed},
		{NavidromeID: "b", Field: "genre", ProposedValue: "Pop",
			Kind: FixKindFill, Confidence: FixConfidenceExact, Status: FixStatusProposed},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	n, err := db.ApproveSafeFixes()
	if err != nil {
		t.Fatalf("ApproveSafeFixes: %v", err)
	}
	if n != 1 {
		t.Errorf("approved %d, want only the genre fix", n)
	}

	var titleStatus string
	if err := db.QueryRow(
		`SELECT status FROM library_fixes WHERE field='title'`).Scan(&titleStatus); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if titleStatus != FixStatusProposed {
		t.Errorf("title status = %q; it must stay for review", titleStatus)
	}
}
