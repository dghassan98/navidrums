package store

import (
	"testing"

	"github.com/cesargomez89/navidrums/internal/domain"
)

func TestOwnedAlbumIDs(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	seed := []*domain.Track{
		{ProviderID: "t1", Title: "One", AlbumID: "alb-1", Status: domain.TrackStatusCompleted},
		{ProviderID: "t2", Title: "Two", AlbumID: "alb-1", Status: domain.TrackStatusCompleted},
		{ProviderID: "t3", Title: "Three", AlbumID: "alb-2", Status: domain.TrackStatusCompleted},
	}
	for _, track := range seed {
		if err := db.CreateTrack(track); err != nil {
			t.Fatalf("CreateTrack failed: %v", err)
		}
	}

	t.Run("reports only the albums present", func(t *testing.T) {
		owned, err := db.OwnedAlbumIDs([]string{"alb-1", "alb-2", "alb-3"})
		if err != nil {
			t.Fatalf("OwnedAlbumIDs failed: %v", err)
		}
		if !owned["alb-1"] || !owned["alb-2"] {
			t.Errorf("owned = %v, want alb-1 and alb-2", owned)
		}
		if owned["alb-3"] {
			t.Error("alb-3 was never downloaded and must not be reported owned")
		}
	})

	t.Run("deduplicates albums with several tracks", func(t *testing.T) {
		owned, err := db.OwnedAlbumIDs([]string{"alb-1"})
		if err != nil {
			t.Fatalf("OwnedAlbumIDs failed: %v", err)
		}
		if len(owned) != 1 {
			t.Errorf("owned = %v, want a single entry", owned)
		}
	})

	t.Run("an empty request does not build IN ()", func(t *testing.T) {
		owned, err := db.OwnedAlbumIDs(nil)
		if err != nil {
			t.Fatalf("OwnedAlbumIDs(nil) failed: %v", err)
		}
		if len(owned) != 0 {
			t.Errorf("owned = %v, want empty", owned)
		}
	})
}
