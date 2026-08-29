package app

import (
	"testing"

	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/store"
)

func TestProposeFixesFillsOnlyEmptyTags(t *testing.T) {
	track := store.LibraryTrack{NavidromeID: "n1", Genre: "", Year: 0}
	match := domain.CatalogTrack{ID: "q1", Genre: "Raï", Year: 1996}

	fixes := ProposeFixes(track, match, store.FixConfidenceExact)

	byField := map[string]store.LibraryFix{}
	for _, f := range fixes {
		byField[f.Field] = f
	}

	if got := byField[FieldGenre]; got.ProposedValue != "Raï" || got.Kind != store.FixKindFill {
		t.Errorf("genre fix = %+v, want a fill of Raï", got)
	}
	if got := byField[FieldYear]; got.ProposedValue != "1996" || got.Kind != store.FixKindFill {
		t.Errorf("year fix = %+v, want a fill of 1996", got)
	}
}

func TestProposeFixesMarksDisagreementsAsChanges(t *testing.T) {
	// Overwriting an existing value is riskier than filling a blank, so it has
	// to be distinguishable: only fills are eligible for unattended apply.
	track := store.LibraryTrack{NavidromeID: "n1", Genre: "Pop", Year: 1995}
	match := domain.CatalogTrack{ID: "q1", Genre: "Raï", Year: 1996}

	for _, f := range ProposeFixes(track, match, store.FixConfidenceExact) {
		if f.Kind != store.FixKindChange {
			t.Errorf("%s was proposed as %q, want a change", f.Field, f.Kind)
		}
	}
}

func TestProposeFixesNeverBlanksATag(t *testing.T) {
	// An empty catalog value is missing information, not a reason to erase a
	// tag the library already has.
	track := store.LibraryTrack{
		NavidromeID: "n1", ISRC: "USABC1234567", Genre: "Raï",
		Year: 1996, TrackNumber: 3, DiscNumber: 1,
	}
	match := domain.CatalogTrack{ID: "q1"} // everything empty

	if fixes := ProposeFixes(track, match, store.FixConfidenceExact); len(fixes) != 0 {
		t.Errorf("proposed %d fixes from an empty source: %+v", len(fixes), fixes)
	}
}

func TestProposeFixesIgnoresIdenticalValues(t *testing.T) {
	track := store.LibraryTrack{NavidromeID: "n1", Genre: "Raï", Year: 1996, TrackNumber: 3}
	match := domain.CatalogTrack{ID: "q1", Genre: "Raï", Year: 1996, TrackNumber: 3}

	if fixes := ProposeFixes(track, match, store.FixConfidenceExact); len(fixes) != 0 {
		t.Errorf("proposed %d fixes for identical data: %+v", len(fixes), fixes)
	}
}

func TestProposeFixesNeverTouchesIdentityFields(t *testing.T) {
	// Artist, album and title are populated on every track in the library.
	// Rewriting a correct name from a match is how a cleanup does damage, so
	// no proposal may ever name one of these fields.
	track := store.LibraryTrack{
		NavidromeID: "n1",
		Title:       "Wahrane Wahrane", Artist: "Khaled", Album: "Sahra",
	}
	match := domain.CatalogTrack{
		ID: "q1", Title: "Wahrane Wahrane (Remastered)",
		Artist: "Cheb Khaled", Album: "Sahra (Deluxe)", Genre: "Raï",
	}

	for _, f := range ProposeFixes(track, match, store.FixConfidenceExact) {
		switch f.Field {
		case "title", "artist", "album", "album_artist":
			t.Errorf("proposed a change to identity field %q", f.Field)
		}
	}
}

func TestProposeFixesTreatsZeroNumbersAsAbsent(t *testing.T) {
	// No track is track zero: a 0 means the tag is missing, and must not be
	// reported as a value that disagrees.
	track := store.LibraryTrack{NavidromeID: "n1", TrackNumber: 0, DiscNumber: 0}
	match := domain.CatalogTrack{ID: "q1", TrackNumber: 5, DiscNumber: 2}

	for _, f := range ProposeFixes(track, match, store.FixConfidenceExact) {
		if f.Kind != store.FixKindFill {
			t.Errorf("%s = %q, want fill: zero means absent", f.Field, f.Kind)
		}
		if f.CurrentValue != "" {
			t.Errorf("%s current = %q, want empty", f.Field, f.CurrentValue)
		}
	}
}

func TestProposeFixesCarriesConfidenceAndSource(t *testing.T) {
	track := store.LibraryTrack{NavidromeID: "n1"}
	match := domain.CatalogTrack{ID: "q42", Genre: "Raï"}

	fixes := ProposeFixes(track, match, store.FixConfidenceFuzzy)
	if len(fixes) != 1 {
		t.Fatalf("got %d fixes, want 1", len(fixes))
	}
	if fixes[0].Confidence != store.FixConfidenceFuzzy {
		t.Errorf("confidence = %q", fixes[0].Confidence)
	}
	if fixes[0].SourceTrackID != "q42" {
		t.Errorf("source = %q, want the catalog track id", fixes[0].SourceTrackID)
	}
	if fixes[0].Status != store.FixStatusProposed {
		t.Errorf("status = %q, want proposed", fixes[0].Status)
	}
}

func TestIntTagTreatsZeroAsAbsent(t *testing.T) {
	if got := intTag(0); got != "" {
		t.Errorf("intTag(0) = %q, want empty", got)
	}
	if got := intTag(-1); got != "" {
		t.Errorf("intTag(-1) = %q, want empty", got)
	}
	if got := intTag(7); got != "7" {
		t.Errorf("intTag(7) = %q", got)
	}
}

func TestProposeFixesIgnoresCaseOnlyDifferences(t *testing.T) {
	// The dry run proposed rewriting "Ussm11905848" as "USSM11905848" — a file
	// write that changes nothing but the casing of an identifier.
	track := store.LibraryTrack{NavidromeID: "n1", ISRC: "Ussm11905848", Genre: "raï"}
	match := domain.CatalogTrack{ID: "q1", ISRC: "USSM11905848", Genre: "Raï"}

	if fixes := ProposeFixes(track, match, store.FixConfidenceExact); len(fixes) != 0 {
		t.Errorf("proposed %d case-only changes: %+v", len(fixes), fixes)
	}
}
