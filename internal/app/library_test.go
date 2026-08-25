package app

import (
	"testing"

	"github.com/cesargomez89/navidrums/internal/subsonic"
)

func TestToLibraryTrackNormalisesKeys(t *testing.T) {
	track := toLibraryTrack(subsonic.Song{
		ID:     "abc",
		Title:  "Bohemian Rhapsody (Remastered 2011)",
		Artist: "Queen",
		Album:  "A Night at the Opera [Deluxe]",
		ISRC:   "GBUM71029604",
		Suffix: "FLAC",
	})

	if track.TitleKey != "bohemian rhapsody" {
		t.Errorf("TitleKey = %q", track.TitleKey)
	}
	if track.ArtistKey != "queen" {
		t.Errorf("ArtistKey = %q", track.ArtistKey)
	}
	if track.AlbumKey != "a night at the opera" {
		t.Errorf("AlbumKey = %q", track.AlbumKey)
	}
	// The display values must survive untouched: the keys are for matching,
	// not for showing.
	if track.Title != "Bohemian Rhapsody (Remastered 2011)" {
		t.Errorf("Title was mangled: %q", track.Title)
	}
}

func TestIsLosslessByFormat(t *testing.T) {
	lossless := []string{"flac", "FLAC", "alac", "wav", "aiff"}
	lossy := []string{"mp3", "m4a", "opus", "ogg", "aac", ""}

	for _, s := range lossless {
		if !toLibraryTrack(subsonic.Song{Suffix: s, Lossless: true}).Lossless {
			t.Errorf("%q should be lossless", s)
		}
	}
	for _, s := range lossy {
		if toLibraryTrack(subsonic.Song{Suffix: s, Lossless: false}).Lossless {
			t.Errorf("%q should be lossy", s)
		}
	}
}

func TestAlbumOwnershipCompleteness(t *testing.T) {
	tests := []struct {
		name     string
		owned    AlbumOwnership
		complete bool
		upgrade  bool
	}{
		{"nothing owned", AlbumOwnership{Owned: 0, Total: 12}, false, false},
		{"partial, all lossless", AlbumOwnership{Owned: 3, Total: 12, Lossless: 3}, false, false},
		{"partial, some lossy", AlbumOwnership{Owned: 3, Total: 12, Lossless: 1}, false, true},
		{"complete lossless", AlbumOwnership{Owned: 12, Total: 12, Lossless: 12}, true, false},
		{"complete but lossy", AlbumOwnership{Owned: 12, Total: 12, Lossless: 0}, true, true},
		{"empty album", AlbumOwnership{}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.owned.Complete(); got != tt.complete {
				t.Errorf("Complete() = %v, want %v", got, tt.complete)
			}
			if got := tt.owned.UpgradeWorthwhile(); got != tt.upgrade {
				t.Errorf("UpgradeWorthwhile() = %v, want %v", got, tt.upgrade)
			}
		})
	}
}

// TestCompleteButLossyStillInvitesDownload is the behaviour that matters for a
// library that is 45% lossy: holding every track of an album as 128k MP3 must
// not read as "already have it".
func TestCompleteButLossyStillInvitesDownload(t *testing.T) {
	owned := AlbumOwnership{Owned: 10, Total: 10, Lossless: 0}

	if !owned.Complete() {
		t.Error("all ten tracks are present, so it is complete")
	}
	if !owned.UpgradeWorthwhile() {
		t.Error("a fully lossy album must still read as worth downloading")
	}
}

func TestUnconfiguredLibraryServiceIsInert(t *testing.T) {
	// No Navidrome configured must mean no marks, never an error or a panic.
	s := NewLibraryService(subsonic.NewClient("", "", ""), nil, nil)

	if s.Configured() {
		t.Error("an empty client reported itself configured")
	}
	owned, err := s.OwnershipFor(nil)
	if err != nil {
		t.Errorf("OwnershipFor returned an error: %v", err)
	}
	if owned.Total != 0 || owned.Owned != 0 {
		t.Errorf("owned = %+v, want zero", owned)
	}
}
