package httpapp

import (
	"html"
	"strings"
)

// PlainText renders provider prose as readable plain text.
//
// Qobuz biographies and playlist descriptions arrive as HTML fragments —
// "<i>Lungs</i>", "&amp;", the odd "<br>". Go's templates escape that, so it
// reaches the page as literal markup. Rendering it as HTML instead would mean
// trusting third-party content in the page, so the tags are removed and the
// entities decoded, leaving text that reads properly.
//
// Block-level tags become spaces so sentences do not run together; inline tags
// vanish silently so "<i>Lungs</i>" reads as "Lungs".
//
// This is a stripper, not a parser: text containing both '<' and a later '>'
// ("5 < 10 > 3") has the span between them removed. That trade is deliberate —
// provider prose is HTML, and mistaking prose for markup is rarer than the
// reverse.
func PlainText(s string) string {
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}

		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			// A lone '<' is literal text, not a tag that lost its closer.
			// Dropping the rest would silently truncate a description.
			b.WriteString(s[i:])
			break
		}

		if spacesOut(s[i+1 : i+end]) {
			b.WriteByte(' ')
		}
		i += end + 1
	}

	return strings.TrimSpace(collapseSpaces(html.UnescapeString(b.String())))
}

// blockTags separate blocks of text, so removing them outright would join the
// surrounding words. Everything else is inline and leaves no trace.
var blockTags = map[string]bool{
	"br": true, "p": true, "div": true, "li": true, "tr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

func spacesOut(tag string) bool {
	tag = strings.TrimSpace(strings.ToLower(tag))
	tag = strings.TrimPrefix(tag, "/")
	if idx := strings.IndexAny(tag, " \t\n/"); idx >= 0 {
		tag = tag[:idx]
	}
	return blockTags[tag]
}

func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}
