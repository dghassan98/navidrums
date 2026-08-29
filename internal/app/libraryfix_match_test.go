package app

import (
	"context"
	"testing"

	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/store"
)

type stubSearcher struct {
	byQuery map[string][]domain.CatalogTrack
	queries []string
}

func (s *stubSearcher) Search(_ context.Context, query, _ string) (*domain.SearchResult, error) {
	s.queries = append(s.queries, query)
	return &domain.SearchResult{Tracks: s.byQuery[query]}, nil
}

func newFixService(stub *stubSearcher) *LibraryFixService {
	return NewLibraryFixService(func() CatalogSearcher { return stub }, nil, nil)
}

func TestFindCatalogTrackPrefersISRC(t *testing.T) {
	stub := &stubSearcher{byQuery: map[string][]domain.CatalogTrack{
		"USABC1234567": {{ID: "q1", Title: "Anything", Artist: "Whoever", ISRC: "USABC1234567"}},
	}}
	svc := newFixService(stub)

	track := store.LibraryTrack{
		ISRC: "USABC1234567", Title: "Fever", Artist: "Dua Lipa",
		TitleKey: "fever", ArtistKey: "dua lipa",
	}

	match, confidence := svc.findCatalogTrack(context.Background(), track)
	if match == nil || match.ID != "q1" {
		t.Fatalf("match = %+v, want the ISRC hit", match)
	}
	if confidence != store.FixConfidenceExact {
		t.Errorf("confidence = %q, want exact", confidence)
	}
	if len(stub.queries) != 1 {
		t.Errorf("queries = %v; a successful ISRC lookup should not fall through to a name search", stub.queries)
	}
}

func TestFindCatalogTrackRejectsWrongISRC(t *testing.T) {
	// A search by ISRC can return near matches. Only an exact ISRC counts as
	// exact, otherwise "safe to apply unattended" would not be safe.
	stub := &stubSearcher{byQuery: map[string][]domain.CatalogTrack{
		"USABC1234567": {{ID: "wrong", ISRC: "GBXYZ9999999"}},
		"Dua Lipa Fever": {{
			ID: "q2", Title: "Fever", Artist: "Dua Lipa", ISRC: "GBXYZ9999999",
		}},
	}}
	svc := newFixService(stub)

	track := store.LibraryTrack{
		ISRC: "USABC1234567", Title: "Fever", Artist: "Dua Lipa",
		TitleKey: "fever", ArtistKey: "dua lipa",
	}

	match, confidence := svc.findCatalogTrack(context.Background(), track)
	if match == nil || match.ID != "q2" {
		t.Fatalf("match = %+v, want the name-matched track", match)
	}
	if confidence != store.FixConfidenceFuzzy {
		t.Errorf("confidence = %q; a non-matching ISRC must not count as exact", confidence)
	}
}

func TestFindCatalogTrackMatchesOnLeadArtist(t *testing.T) {
	// The Fever case: the library credits "Dua Lipa, Angèle", the catalog
	// credits "Dua Lipa".
	stub := &stubSearcher{byQuery: map[string][]domain.CatalogTrack{
		"Dua Lipa, Angèle Fever": {{ID: "q3", Title: "Fever", Artist: "Dua Lipa"}},
	}}
	svc := newFixService(stub)

	track := store.LibraryTrack{
		Title: "Fever", Artist: "Dua Lipa, Angèle",
		TitleKey: "fever", ArtistKey: "dua lipa angele", ArtistPrimaryKey: "dua lipa",
	}

	match, confidence := svc.findCatalogTrack(context.Background(), track)
	if match == nil {
		t.Fatal("no match; the lead-artist key should have found it")
	}
	if confidence != store.FixConfidenceFuzzy {
		t.Errorf("confidence = %q, want fuzzy", confidence)
	}
}

func TestFindCatalogTrackRejectsADifferentSong(t *testing.T) {
	// A wrong match here writes wrong tags to a real file, so a title that
	// does not agree must not match however plausible the artist.
	stub := &stubSearcher{byQuery: map[string][]domain.CatalogTrack{
		"Khaled Sahra": {{ID: "q4", Title: "Aicha", Artist: "Khaled"}},
	}}
	svc := newFixService(stub)

	track := store.LibraryTrack{
		Title: "Sahra", Artist: "Khaled",
		TitleKey: "sahra", ArtistKey: "khaled",
	}

	if match, _ := svc.findCatalogTrack(context.Background(), track); match != nil {
		t.Errorf("matched %+v; a different song must not match", match)
	}
}

func TestFindCatalogTrackHandlesNoProvider(t *testing.T) {
	svc := NewLibraryFixService(func() CatalogSearcher { return nil }, nil, nil)

	track := store.LibraryTrack{Title: "Sahra", Artist: "Khaled", TitleKey: "sahra", ArtistKey: "khaled"}
	if match, _ := svc.findCatalogTrack(context.Background(), track); match != nil {
		t.Error("matched despite there being no provider")
	}
}
