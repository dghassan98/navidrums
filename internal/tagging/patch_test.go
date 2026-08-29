package tagging

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-flac/flacvorbis"
	"github.com/go-flac/go-flac"
)

// writeTestFLAC builds a minimal but valid FLAC carrying the given comments,
// so a patch can be proved not to disturb the ones it was not asked to change.
func writeTestFLAC(t *testing.T, path string, comments map[string]string) {
	t.Helper()

	f := new(flac.File)
	streamInfo := flac.MetaDataBlock{Type: flac.StreamInfo, Data: make([]byte, 34)}
	f.Meta = append(f.Meta, &streamInfo)

	vc := flacvorbis.New()
	for key, value := range comments {
		if err := vc.Add(key, value); err != nil {
			t.Fatalf("adding %s: %v", key, err)
		}
	}
	block := vc.Marshal()
	f.Meta = append(f.Meta, &block)
	f.Frames = []byte{0xFF, 0xF8, 0x00, 0x00}

	if err := f.Save(path); err != nil {
		t.Fatalf("writing test FLAC: %v", err)
	}
}

func readAllFLACComments(t *testing.T, path string) map[string]string {
	t.Helper()

	f, err := flac.ParseFile(path)
	if err != nil {
		t.Fatalf("parsing FLAC: %v", err)
	}

	out := map[string]string{}
	for _, block := range f.Meta {
		if block.Type != flac.VorbisComment {
			continue
		}
		vc, err := flacvorbis.ParseFromMetaDataBlock(*block)
		if err != nil {
			t.Fatalf("parsing comments: %v", err)
		}
		for _, comment := range vc.Comments {
			for i := 0; i < len(comment); i++ {
				if comment[i] == '=' {
					out[comment[:i]] = comment[i+1:]
					break
				}
			}
		}
	}
	return out
}

// TestPatchFLACPreservesUnrelatedTags is the guarantee the whole cleanup rests
// on. Editing one tag must not disturb anything else on the file — the previous
// writer rebuilt the comment block from the handful of fields Navidrums models,
// which would have silently destroyed the rest.
func TestPatchFLACPreservesUnrelatedTags(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")

	writeTestFLAC(t, path, map[string]string{
		"TITLE":                 "One Last Time (EDIT 4K)",
		"ARTIST":                "Ariana Grande",
		"ALBUM":                 "My Everything",
		"GENRE":                 "",
		"COMMENT":               "ripped by me, do not lose this",
		"LYRICS":                "I was a liar...",
		"REPLAYGAIN_TRACK_GAIN": "-7.86 dB",
		"REPLAYGAIN_TRACK_PEAK": "0.988",
		"GROUPING":              "favourites",
		"MUSICBRAINZ_TRACKID":   "abc-123",
		"SOMETHING_CUSTOM":      "keep me too",
	})

	if err := PatchTags(path, map[string]string{
		PatchGenre: "Pop",
		PatchTitle: "One Last Time",
	}); err != nil {
		t.Fatalf("PatchTags: %v", err)
	}

	after := readAllFLACComments(t, path)

	if after["GENRE"] != "Pop" {
		t.Errorf("GENRE = %q, want Pop", after["GENRE"])
	}
	if after["TITLE"] != "One Last Time" {
		t.Errorf("TITLE = %q", after["TITLE"])
	}

	// Everything not named in the patch must be exactly as it was.
	untouched := map[string]string{
		"ARTIST":                "Ariana Grande",
		"ALBUM":                 "My Everything",
		"COMMENT":               "ripped by me, do not lose this",
		"LYRICS":                "I was a liar...",
		"REPLAYGAIN_TRACK_GAIN": "-7.86 dB",
		"REPLAYGAIN_TRACK_PEAK": "0.988",
		"GROUPING":              "favourites",
		"MUSICBRAINZ_TRACKID":   "abc-123",
		"SOMETHING_CUSTOM":      "keep me too",
	}
	for key, want := range untouched {
		if got := after[key]; got != want {
			t.Errorf("%s = %q, want %q — an unrelated tag was lost or altered", key, got, want)
		}
	}
}

func TestPatchFLACDoesNotDuplicateAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")
	writeTestFLAC(t, path, map[string]string{"TITLE": "Old", "ARTIST": "A"})

	if err := PatchTags(path, map[string]string{PatchTitle: "New"}); err != nil {
		t.Fatalf("PatchTags: %v", err)
	}
	if err := PatchTags(path, map[string]string{PatchTitle: "Newer"}); err != nil {
		t.Fatalf("second PatchTags: %v", err)
	}

	f, err := flac.ParseFile(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	titles := 0
	for _, block := range f.Meta {
		if block.Type != flac.VorbisComment {
			continue
		}
		vc, _ := flacvorbis.ParseFromMetaDataBlock(*block)
		for _, c := range vc.Comments {
			if len(c) >= 6 && c[:6] == "TITLE=" {
				titles++
			}
		}
	}
	if titles != 1 {
		t.Errorf("found %d TITLE comments, want exactly 1", titles)
	}
}

func TestReadTagsReportsCurrentValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")
	writeTestFLAC(t, path, map[string]string{
		"TITLE": "Sahra", "GENRE": "World Music", "DATE": "1996",
		"TRACKNUMBER": "3", "ISRC": "USABC1234567",
	})

	got, err := ReadTags(path)
	if err != nil {
		t.Fatalf("ReadTags: %v", err)
	}

	want := map[string]string{
		PatchTitle: "Sahra", PatchGenre: "World Music", PatchYear: "1996",
		PatchTrackNumber: "3", PatchISRC: "USABC1234567",
	}
	for field, expected := range want {
		if got[field] != expected {
			t.Errorf("%s = %q, want %q", field, got[field], expected)
		}
	}
}

// TestPatchRefusesFormatsItCannotEditSafely covers formats with neither a
// pure-Go writer nor an ffmpeg route. Falling back to the full rewriter would
// replace every tag, so refusing is the safe answer.
func TestPatchRefusesFormatsItCannotEditSafely(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"d.wav", "e.wma", "f.aiff"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("not really audio"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		err := PatchTags(path, map[string]string{PatchGenre: "Pop"})
		if !errors.Is(err, ErrPatchUnsupported) {
			t.Errorf("PatchTags(%s) = %v, want ErrPatchUnsupported", name, err)
		}
	}
}

func TestPatchRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")
	writeTestFLAC(t, path, map[string]string{"TITLE": "x"})

	// Artist and album are never cleanup targets; a typo must not silently
	// write a stray comment.
	if err := PatchTags(path, map[string]string{"artist": "Someone Else"}); err == nil {
		t.Error("an unknown field was accepted")
	}
}

func TestPatchLeavesTheFileIntactOnNoChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.flac")
	writeTestFLAC(t, path, map[string]string{"TITLE": "x"})

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := PatchTags(path, nil); err != nil {
		t.Fatalf("PatchTags(nil): %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(before) != len(after) {
		t.Error("an empty patch rewrote the file")
	}
}

// TestOpusAndM4AGoThroughFFmpeg records the decision that these are retagged
// rather than converted. Opus carries Vorbis comments natively, so converting
// it to a "taggable" format would re-encode lossy audio into lossy audio and
// lose real quality to solve a metadata problem. ffmpeg copies the streams
// across untouched instead.
//
// Without ffmpeg present the attempt reports that plainly rather than leaving
// those files quietly unfixed.
func TestOpusAndM4AGoThroughFFmpeg(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"a.opus", "b.m4a"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("not really audio"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}

		err := PatchTags(path, map[string]string{PatchGenre: "Pop"})
		if err == nil {
			t.Errorf("PatchTags(%s) unexpectedly succeeded on a stub file", name)
			continue
		}
		// These must never be reported as unsupported: they have a route.
		if errors.Is(err, ErrPatchUnsupported) {
			t.Errorf("%s was reported unsupported; it should go through ffmpeg", name)
		}
	}
}

// TestFLACFallsBackWhenTheDirectoryIsReadOnly covers the failure seen on the
// real library: MP3s succeeded while FLACs in the same tree failed with
// "permission denied", because creating a temporary file needs write
// permission on the directory while editing an MP3 in place does not.
func TestFLACFallsBackWhenTheDirectoryIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, which ignores directory permissions")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "song.flac")
	writeTestFLAC(t, path, map[string]string{
		"TITLE": "Old", "COMMENT": "keep me",
	})

	// The file stays writable; the directory does not accept new entries.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("cannot make the directory read-only here: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()

	if err := PatchTags(path, map[string]string{PatchTitle: "New"}); err != nil {
		t.Fatalf("PatchTags should have fallen back to an in-place write: %v", err)
	}

	after := readAllFLACComments(t, path)
	if after["TITLE"] != "New" {
		t.Errorf("TITLE = %q, want New", after["TITLE"])
	}
	if after["COMMENT"] != "keep me" {
		t.Errorf("COMMENT = %q; unrelated tags must survive the fallback too", after["COMMENT"])
	}
}
