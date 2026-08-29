package artwork

import (
	"context"
	"errors"
	"testing"
)

type stubSource struct {
	name  string
	found []Candidate
	err   error
}

func (s stubSource) Name() string { return s.name }
func (s stubSource) Search(context.Context, string, string) ([]Candidate, error) {
	return s.found, s.err
}

func TestSearchMergesEverySource(t *testing.T) {
	f := NewFinder(10,
		stubSource{name: "a", found: []Candidate{{Source: "a", ImageURL: "1", Size: 500}}},
		stubSource{name: "b", found: []Candidate{{Source: "b", ImageURL: "2", Size: 900}}},
	)

	got := f.Search(context.Background(), "Khaled", "Sahra")
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want both sources merged", len(got))
	}
}

func TestSearchSurvivesAFailingSource(t *testing.T) {
	// A cover from somewhere beats an error because one service was down.
	f := NewFinder(10,
		stubSource{name: "broken", err: errors.New("unreachable")},
		stubSource{name: "ok", found: []Candidate{{Source: "ok", ImageURL: "1"}}},
	)

	got := f.Search(context.Background(), "Khaled", "Sahra")
	if len(got) != 1 {
		t.Fatalf("got %d, want the working source's result", len(got))
	}
}

func TestSearchPutsLargestFirst(t *testing.T) {
	// Changing a cover is usually about getting a better one.
	f := NewFinder(10, stubSource{name: "a", found: []Candidate{
		{ImageURL: "small", Size: 300},
		{ImageURL: "big", Size: 1000},
		{ImageURL: "mid", Size: 600},
	}})

	got := f.Search(context.Background(), "x", "y")
	if got[0].ImageURL != "big" || got[2].ImageURL != "small" {
		t.Errorf("order = %v", []string{got[0].ImageURL, got[1].ImageURL, got[2].ImageURL})
	}
}

func TestSearchDropsDuplicatesAndEmpties(t *testing.T) {
	f := NewFinder(10,
		stubSource{name: "a", found: []Candidate{{ImageURL: "same"}, {ImageURL: ""}}},
		stubSource{name: "b", found: []Candidate{{ImageURL: "same"}}},
	)

	got := f.Search(context.Background(), "x", "y")
	if len(got) != 1 {
		t.Errorf("got %d, want one after removing the duplicate and the empty", len(got))
	}
}

func TestSearchCapsResults(t *testing.T) {
	// The point is choosing by eye, so the grid must stay a grid.
	many := make([]Candidate, 40)
	for i := range many {
		many[i] = Candidate{ImageURL: string(rune('a'+i%26)) + string(rune('0'+i/26))}
	}
	f := NewFinder(5, stubSource{name: "a", found: many})

	if got := f.Search(context.Background(), "x", "y"); len(got) != 5 {
		t.Errorf("got %d, want the limit of 5", len(got))
	}
}

func TestThumbFallsBackToFullImage(t *testing.T) {
	f := NewFinder(5, stubSource{name: "a", found: []Candidate{{ImageURL: "full"}}})

	got := f.Search(context.Background(), "x", "y")
	if got[0].ThumbURL != "full" {
		t.Errorf("thumb = %q; a source without one must still render", got[0].ThumbURL)
	}
}
