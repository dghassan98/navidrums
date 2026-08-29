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
