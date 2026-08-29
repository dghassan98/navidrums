package tagging

import (
	"strings"
	"testing"
)

func TestCapabilitiesAlwaysCoversNativeFormats(t *testing.T) {
	// FLAC and MP3 are handled in-process, so they must report available
	// whatever else is missing from the container.
	got := map[string]FormatSupport{}
	for _, c := range Capabilities() {
		got[c.Extension] = c
	}

	for _, ext := range []string{".flac", ".mp3"} {
		if !got[ext].Available {
			t.Errorf("%s reported unavailable; it needs no external tool", ext)
		}
		if got[ext].Route != "built in" {
			t.Errorf("%s route = %q", ext, got[ext].Route)
		}
	}
}

func TestCapabilitiesExplainsAMissingTool(t *testing.T) {
	// A gap discovered halfway through writing to a library is far worse than
	// one shown beforehand, so an unavailable format must say why.
	for _, c := range Capabilities() {
		if !c.Available && strings.TrimSpace(c.Reason) == "" {
			t.Errorf("%s is unavailable but gives no reason", c.Extension)
		}
	}
}

func TestCanPatchMatchesCapabilities(t *testing.T) {
	if ok, _ := CanPatch("/music/Library/A/B/song.flac"); !ok {
		t.Error("a FLAC should always be patchable")
	}
	if ok, _ := CanPatch("/music/Library/A/B/song.MP3"); !ok {
		t.Error("extension matching should ignore case")
	}
	if ok, reason := CanPatch("/music/Library/A/B/song.wma"); ok {
		t.Error("an unsupported format reported patchable")
	} else if reason == "" {
		t.Error("an unsupported format gave no reason")
	}
}
