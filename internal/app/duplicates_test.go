package app

import (
	"testing"

	"github.com/cesargomez89/navidrums/internal/store"
)

// TestPlaylistMembershipOutranksQuality is the rule that protects the thing a
// person built by hand. A file referenced by a playlist must never rank below
// one that merely sounds better, because deleting it silently breaks the
// playlist and no bitrate compensates for that.
func TestPlaylistMembershipOutranksQuality(t *testing.T) {
	inPlaylist := store.DuplicateCopy{
		Suffix: "mp3", BitRate: 192, Playlists: []string{"Cooking Mix"},
	}
	betterAudio := store.DuplicateCopy{
		Suffix: "flac", BitRate: 1000, Lossless: true,
		ISRC: "X", Genre: "Pop", Year: 2020, TrackNumber: 1, DiscNumber: 1,
	}

	if ScoreCopy(inPlaylist).Score <= ScoreCopy(betterAudio).Score {
		t.Error("a playlist reference must outrank a better-sounding copy")
	}
}

func TestLosslessOutranksLossy(t *testing.T) {
	lossy := store.DuplicateCopy{Suffix: "mp3", BitRate: 320}
	lossless := store.DuplicateCopy{Suffix: "flac", BitRate: 900, Lossless: true}

	if ScoreCopy(lossless).Score <= ScoreCopy(lossy).Score {
		t.Error("a lossless copy should rank above a lossy one")
	}
}

func TestMetadataBreaksTiesBetweenEqualCopies(t *testing.T) {
	bare := store.DuplicateCopy{Suffix: "flac", BitRate: 900, Lossless: true}
	tagged := store.DuplicateCopy{
		Suffix: "flac", BitRate: 900, Lossless: true,
		ISRC: "X", Genre: "Pop", Year: 1996, TrackNumber: 3, DiscNumber: 1,
	}

	if ScoreCopy(tagged).Score <= ScoreCopy(bare).Score {
		t.Error("the better-tagged copy should win a tie on quality")
	}
}

func TestBitrateNeverOutranksLossless(t *testing.T) {
	// Bitrate is scaled down deliberately: a very high lossy bitrate must not
	// beat a lossless copy just by being a large number.
	loudLossy := store.DuplicateCopy{Suffix: "mp3", BitRate: 3200}
	quietLossless := store.DuplicateCopy{Suffix: "flac", BitRate: 400, Lossless: true}

	if ScoreCopy(loudLossy).Score >= ScoreCopy(quietLossless).Score {
		t.Error("a high lossy bitrate outranked a lossless copy")
	}
}

func TestScoreExplainsItself(t *testing.T) {
	// The score orders the list, but a person decides. It is useless unless it
	// says why.
	score := ScoreCopy(store.DuplicateCopy{
		Suffix: "flac", BitRate: 900, Lossless: true,
		Playlists: []string{"all timers"}, Genre: "Pop", Size: 40 << 20,
	})

	if len(score.Reasons) == 0 {
		t.Fatal("no reasons given")
	}

	joined := ""
	for _, r := range score.Reasons {
		joined += r + " "
	}
	for _, want := range []string{"playlist", "lossless", "tags", "MB"} {
		if !contains(joined, want) {
			t.Errorf("reasons %q do not mention %q", joined, want)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestHumanSize(t *testing.T) {
	for _, tt := range []struct {
		in   int64
		want string
	}{
		{40 << 20, "40.0 MB"},
		{512 << 10, "512 KB"},
		{900, "900 B"},
	} {
		if got := humanSize(tt.in); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
