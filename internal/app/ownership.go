package app

import (
	"github.com/cesargomez89/navidrums/internal/domain"
)

// OwnershipIndex answers "have we already got this album?" for a page of
// browse results.
//
// It is an interface because the tracks table is only one possible answer: it
// knows every album Navidrums downloaded, but nothing about music that reached
// the library some other way. A read-only library index can be added behind
// this seam without any handler or template changing.
type OwnershipIndex interface {
	OwnedAlbumIDs(albumIDs []string) (map[string]bool, error)
}

// MarkOwned sets Owned on every album the index recognises. One batched lookup
// per row, not one per card.
//
// A failing index is not worth failing a page over: the badge is an
// affordance, not the content, so albums simply render unmarked.
func MarkOwned(index OwnershipIndex, albums []domain.Album) {
	if index == nil || len(albums) == 0 {
		return
	}

	ids := make([]string, 0, len(albums))
	for i := range albums {
		if albums[i].ID != "" {
			ids = append(ids, albums[i].ID)
		}
	}

	owned, err := index.OwnedAlbumIDs(ids)
	if err != nil {
		return
	}

	for i := range albums {
		albums[i].Owned = owned[albums[i].ID]
	}
}
