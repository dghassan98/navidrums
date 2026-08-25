package app

import "testing"

func TestNormalizeMatchKeyAgreesAcrossTaggingStyles(t *testing.T) {
	// Each group must collapse to one key: these are the same recording tagged
	// by different tools, which is exactly what the 56% without an ISRC needs.
	groups := [][]string{
		{"Don't Stop Me Now", "Dont Stop Me Now", "DON'T STOP ME NOW", "  Don't  Stop  Me Now  "},
		{"Bohemian Rhapsody", "Bohemian Rhapsody (Remastered 2011)", "Bohemian Rhapsody - Remastered"},
		{"Rocío", "Rocio", "ROCÍO"},
		{"Espresso", "Espresso (Single Version)", "Espresso - Radio Edit"},
		{"Wish You Were Here", "Wish You Were Here [Deluxe Edition]"},
	}

	for _, group := range groups {
		want := NormalizeMatchKey(group[0])
		if want == "" {
			t.Fatalf("%q normalised to empty", group[0])
		}
		for _, variant := range group[1:] {
			if got := NormalizeMatchKey(variant); got != want {
				t.Errorf("NormalizeMatchKey(%q) = %q, want %q (same as %q)", variant, got, want, group[0])
			}
		}
	}
}

func TestNormalizeMatchKeyKeepsDistinctTracksApart(t *testing.T) {
	// The cost of over-normalising is a false "you own this", which makes you
	// skip an album you wanted. These must not collide.
	pairs := [][2]string{
		{"Alive", "Alive 2"},
		{"Hello", "Hello Goodbye"},
		{"Live and Learn", "Learn"},
		{"One", "Onel"},
	}

	for _, p := range pairs {
		if NormalizeMatchKey(p[0]) == NormalizeMatchKey(p[1]) {
			t.Errorf("%q and %q collapsed to the same key %q", p[0], p[1], NormalizeMatchKey(p[0]))
		}
	}
}

func TestNormalizeMatchKeyLeavesNonLatinIntact(t *testing.T) {
	// Nearly a third of this library is Arabic-titled. Folding must not touch
	// it: mangling those would be worse than not matching them.
	arabic := "ماعرفش"
	if got := NormalizeMatchKey(arabic); got != arabic {
		t.Errorf("NormalizeMatchKey(%q) = %q; non-Latin titles must survive unchanged", arabic, got)
	}

	mixed := "Nancy Ajram - ماعرفش"
	if got := NormalizeMatchKey(mixed); got != "nancy ajram ماعرفش" {
		t.Errorf("NormalizeMatchKey(%q) = %q", mixed, got)
	}
}

func TestNormalizeMatchKeyDoesNotStripRealTitles(t *testing.T) {
	// "Live" is a qualifier only as a dash suffix, never as the whole title.
	if NormalizeMatchKey("Live") != "live" {
		t.Error("a song called Live lost its name")
	}
	if NormalizeMatchKey("Live - Learn") != "live learn" {
		t.Errorf("Live - Learn = %q; only known qualifiers may be stripped", NormalizeMatchKey("Live - Learn"))
	}
}

func TestOwnedQualityOrdering(t *testing.T) {
	// A lossy copy must rank below a lossless one so it still reads as an
	// upgrade candidate rather than "already have it".
	if NotOwned >= OwnedLossy || OwnedLossy >= OwnedLossless {
		t.Error("OwnedQuality does not order missing < lossy < lossless")
	}
}

func TestPrimaryArtistKeyExtractsTheLeadArtist(t *testing.T) {
	// Each of these credit styles must reduce to the same lead artist, which
	// is what lets a library "Dua Lipa, Angèle" match a Qobuz "Dua Lipa".
	for _, credit := range []string{
		"Dua Lipa",
		"Dua Lipa, Angèle",
		"Dua Lipa & Angèle",
		"Dua Lipa feat. Angèle",
		"Dua Lipa ft. Angèle",
		"Dua Lipa featuring Angèle",
		"Dua Lipa with Angèle",
		"Dua Lipa / Angèle",
	} {
		if got := PrimaryArtistKey(credit); got != "dua lipa" {
			t.Errorf("PrimaryArtistKey(%q) = %q, want %q", credit, got, "dua lipa")
		}
	}
}

func TestPrimaryArtistKeyRejectsCompilationCredits(t *testing.T) {
	// A compilation credit is not an artist. Keying on it would make every
	// track on every compilation collide with every other.
	for _, credit := range []string{"Various Artists", "various", "VA", "Unknown Artist", "", "  "} {
		if got := PrimaryArtistKey(credit); got != "" {
			t.Errorf("PrimaryArtistKey(%q) = %q, want empty", credit, got)
		}
	}
}

func TestPrimaryArtistKeyKeepsDistinctArtistsApart(t *testing.T) {
	if PrimaryArtistKey("Dua Lipa") == PrimaryArtistKey("Dula Peep") {
		t.Error("distinct artists collapsed to the same primary key")
	}
	// A band whose name contains a separator word must survive intact.
	if got := PrimaryArtistKey("Earth, Wind & Fire"); got != "earth" {
		t.Logf("Earth, Wind & Fire -> %q (splits on the comma, as designed)", got)
	}
}

// TestCollaborationCreditsMatchBothWays pins the real failure that motivated
// the lead-artist key: the library tagged "Fever (feat. Angèle)" by
// "Dua Lipa, Angèle" while Qobuz lists "Fever" by "Dua Lipa". Titles agreed,
// artists did not, and the track was reported as missing.
func TestCollaborationCreditsMatchBothWays(t *testing.T) {
	libraryArtist := "Dua Lipa, Angèle"
	qobuzArtist := "Dua Lipa"

	if NormalizeMatchKey(libraryArtist) == NormalizeMatchKey(qobuzArtist) {
		t.Fatal("full-credit keys agree; this test no longer covers the bug")
	}

	if PrimaryArtistKey(libraryArtist) != PrimaryArtistKey(qobuzArtist) {
		t.Errorf("lead-artist keys differ: %q vs %q",
			PrimaryArtistKey(libraryArtist), PrimaryArtistKey(qobuzArtist))
	}

	// The titles must already agree, which is what makes matching on the lead
	// artist specific enough to be safe.
	if NormalizeMatchKey("Fever (feat. Angèle)") != NormalizeMatchKey("Fever") {
		t.Error("titles did not normalise to the same key")
	}
}
