package app

import (
	"strings"
	"unicode"
)

// NormalizeMatchKey reduces a title or artist to something two differently
// tagged copies of the same recording can agree on.
//
// It is deliberately blunt: lowercase, strip accents and punctuation, collapse
// whitespace, and drop the bracketed suffixes that vary between releases
// ("(Remastered 2011)", "[Deluxe Edition]", "- Radio Edit"). What survives is
// the part a human would call the name.
//
// This is only ever a *fallback*. An ISRC match is exact and always preferred;
// this exists because 56% of the library has no ISRC to match on.
func NormalizeMatchKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = stripBracketed(s)
	s = stripTrailingQualifier(s)

	var b strings.Builder
	b.Grow(len(s))
	lastWasSpace := true // leading spaces are dropped
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(foldRune(r))
			lastWasSpace = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if !lastWasSpace {
				b.WriteRune(' ')
				lastWasSpace = true
			}
		default:
			// Punctuation is dropped entirely rather than becoming a space, so
			// "don't" and "dont" agree.
		}
	}

	return strings.TrimSpace(b.String())
}

// stripBracketed removes parenthesised and bracketed segments, which is where
// release-specific noise lives.
func stripBracketed(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// trailingQualifiers are the dash-suffixed variants Qobuz and taggers add.
// Only an exact suffix match is stripped, so a song genuinely called
// "Live - Learn" keeps its name.
var trailingQualifiers = []string{
	"remaster", "remastered", "radio edit", "single version", "album version",
	"mono version", "stereo version", "bonus track", "deluxe edition",
	"explicit", "clean", "original mix",
}

func stripTrailingQualifier(s string) string {
	idx := strings.LastIndex(s, " - ")
	if idx < 0 {
		return s
	}
	tail := strings.TrimSpace(s[idx+3:])
	for _, q := range trailingQualifiers {
		if tail == q || strings.HasPrefix(tail, q+" ") || strings.HasSuffix(tail, " "+q) {
			return strings.TrimSpace(s[:idx])
		}
	}
	return s
}

// foldRune maps the common accented Latin letters onto their base form so
// "Rocío" and "Rocio" agree. Anything outside this table is left alone, which
// matters for the Arabic titles in this library: they are already consistent
// and must not be mangled.
func foldRune(r rune) rune {
	if folded, ok := latinFolds[r]; ok {
		return folded
	}
	return r
}

var latinFolds = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ø': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ñ': 'n', 'ç': 'c', 'ý': 'y', 'ÿ': 'y',
}

// OwnedQuality describes how well a track is already held, which decides
// whether owning it should discourage downloading it again.
type OwnedQuality int

const (
	// NotOwned means no copy was found in the library.
	NotOwned OwnedQuality = iota
	// OwnedLossy means a copy exists but is lossy, so a lossless download is
	// still an upgrade worth offering.
	OwnedLossy
	// OwnedLossless means the existing copy is lossless.
	OwnedLossless
)

func (q OwnedQuality) String() string {
	switch q {
	case OwnedLossless:
		return "lossless"
	case OwnedLossy:
		return "lossy"
	default:
		return "missing"
	}
}
