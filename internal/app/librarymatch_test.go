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
