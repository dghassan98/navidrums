package httpapp

import "testing"

func TestPlainTextStripsProviderMarkup(t *testing.T) {
	// The real case: a Qobuz biography reaching the page as literal markup.
	in := "Im Juni 2008 brachten Florence + The Machine ihre Debüt-Single <i>Kiss with a Fist</i> beim Label Moshi Moshi heraus."
	want := "Im Juni 2008 brachten Florence + The Machine ihre Debüt-Single Kiss with a Fist beim Label Moshi Moshi heraus."

	if got := PlainText(in); got != want {
		t.Errorf("PlainText() = %q, want %q", got, want)
	}
}

func TestPlainTextDecodesEntities(t *testing.T) {
	tests := map[string]string{
		"Rock &amp; Roll":   "Rock & Roll",
		"&quot;Sahra&quot;": `"Sahra"`,
		"caf&eacute;":       "café",
		"a &lt;b&gt; c":     "a <b> c",
	}
	for in, want := range tests {
		if got := PlainText(in); got != want {
			t.Errorf("PlainText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlainTextKeepsBlocksApart(t *testing.T) {
	// Removing a block tag outright would run the sentences together.
	tests := map[string]string{
		"One.<br>Two.":           "One. Two.",
		"<p>One.</p><p>Two.</p>": "One. Two.",
		"<li>a</li><li>b</li>":   "a b",
	}
	for in, want := range tests {
		if got := PlainText(in); got != want {
			t.Errorf("PlainText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPlainTextHandlesInlineTagsWithAttributes(t *testing.T) {
	in := `see <a href="https://example.test" rel="nofollow">this album</a> now`
	if got, want := PlainText(in), "see this album now"; got != want {
		t.Errorf("PlainText() = %q, want %q", got, want)
	}
}

func TestPlainTextLeavesOrdinaryTextAlone(t *testing.T) {
	for _, s := range []string{
		"",
		"A perfectly ordinary description.",
		"Khaled — Sahra (1996)",
		"ماعرفش",
	} {
		if got := PlainText(s); got != s {
			t.Errorf("PlainText(%q) = %q; ordinary text must be unchanged", s, got)
		}
	}
}

func TestPlainTextHandlesUnclosedAngleBracket(t *testing.T) {
	// A lone '<' is literal text, not a tag that lost its closer. Dropping the
	// rest of the string would silently truncate a description.
	in := "rated 5 < 10 overall"
	if got := PlainText(in); got != in {
		t.Errorf("PlainText(%q) = %q, want it unchanged", in, got)
	}
}
