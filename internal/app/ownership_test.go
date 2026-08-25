package app

import (
	"errors"
	"testing"

	"github.com/cesargomez89/navidrums/internal/domain"
)

type stubOwnership struct {
	owned map[string]bool
	asked []string
	err   error
	calls int
}

func (s *stubOwnership) OwnedAlbumIDs(albumIDs []string) (map[string]bool, error) {
	s.calls++
	s.asked = albumIDs
	if s.err != nil {
		return nil, s.err
	}
	return s.owned, nil
}

func TestMarkOwnedFlagsMatchingAlbums(t *testing.T) {
	albums := []domain.Album{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	index := &stubOwnership{owned: map[string]bool{"b": true}}

	MarkOwned(index, albums)

	if albums[0].Owned || albums[2].Owned {
		t.Error("albums not in the index were marked owned")
	}
	if !albums[1].Owned {
		t.Error("the owned album was not marked")
	}
}

func TestMarkOwnedBatchesOneLookup(t *testing.T) {
	// One indexed query per row, not one per card.
	albums := []domain.Album{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	index := &stubOwnership{owned: map[string]bool{}}

	MarkOwned(index, albums)

	if index.calls != 1 {
		t.Errorf("lookups = %d, want 1", index.calls)
	}
	if len(index.asked) != 3 {
		t.Errorf("asked about %d ids, want 3", len(index.asked))
	}
}

func TestMarkOwnedIgnoresIndexFailure(t *testing.T) {
	// The badge is an affordance, not the content: a broken index must leave
	// the page renderable rather than fail it.
	albums := []domain.Album{{ID: "a"}}
	index := &stubOwnership{err: errors.New("database is away")}

	MarkOwned(index, albums)

	if albums[0].Owned {
		t.Error("album was marked owned despite the lookup failing")
	}
}

func TestMarkOwnedHandlesNilIndexAndEmptyInput(t *testing.T) {
	MarkOwned(nil, []domain.Album{{ID: "a"}})

	index := &stubOwnership{owned: map[string]bool{}}
	MarkOwned(index, nil)
	if index.calls != 0 {
		t.Error("an empty album list should not hit the index")
	}
}

func TestMarkOwnedSkipsBlankIDs(t *testing.T) {
	albums := []domain.Album{{ID: ""}, {ID: "b"}}
	index := &stubOwnership{owned: map[string]bool{"b": true}}

	MarkOwned(index, albums)

	if len(index.asked) != 1 || index.asked[0] != "b" {
		t.Errorf("asked = %v, want only the album with an id", index.asked)
	}
}
