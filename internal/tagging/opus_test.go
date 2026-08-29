package tagging

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestOpus builds a synthetic OggOpus file: a header page, a comment
// packet, and a couple of audio pages.
func writeTestOpus(t *testing.T, path string, comments []string, audioPages int) {
	t.Helper()

	const serial = uint32(0xC0FFEE)

	head := append([]byte("OpusHead"), 1, 2)
	head = append(head, make([]byte, 9)...)
	headPage := buildOggPages(head, serial, 0, 0)[0]
	headPage.HeaderType = oggFlagBOS

	tags := marshalOpusComments(opusComments{Vendor: "navidrums-test", Comments: comments})
	pages := append([]oggPage{headPage}, buildOggPages(tags, serial, 1, 0)...)

	for i := range audioPages {
		pages = append(pages, oggPage{
			Serial:  serial,
			Granule: uint64((i + 1) * 960),
			// A short final segment marks the packet complete.
			Segments: []byte{4},
			Body:     []byte{0xAA, 0xBB, 0xCC, 0xDD},
		})
	}

	if err := os.WriteFile(path, marshalOggPages(pages), 0o600); err != nil {
		t.Fatalf("writing test Opus: %v", err)
	}
}

func readOpusCommentList(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	pages, err := parseOggPages(data)
	if err != nil {
		t.Fatalf("parse pages: %v", err)
	}
	comments, _, _, err := readOpusTags(pages)
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	return comments.Comments
}

// TestPatchOpusPreservesCoverArt is the whole reason this exists. ffmpeg cannot
// write an embedded picture back into Ogg, so retagging through it discards the
// artwork. Editing the comment list leaves the picture alone by construction.
func TestPatchOpusPreservesCoverArt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.opus")

	// A picture comment large enough to span several pages, as a real cover is.
	art := "METADATA_BLOCK_PICTURE=" + strings.Repeat("QUJDRA", 30000)
	writeTestOpus(t, path, []string{
		"TITLE=Touch It (Instrumental)",
		"ARTIST=Ariana Grande",
		"COMMENT=do not lose this",
		art,
	}, 3)

	if err := PatchTags(path, map[string]string{
		PatchGenre: "Pop",
		PatchTitle: "Touch It",
	}); err != nil {
		t.Fatalf("PatchTags: %v", err)
	}

	after := readOpusCommentList(t, path)
	joined := strings.Join(after, "\n")

	if !strings.Contains(joined, art) {
		t.Error("the cover art comment was lost or altered")
	}
	if !strings.Contains(joined, "COMMENT=do not lose this") {
		t.Error("an unrelated comment was lost")
	}
	if !strings.Contains(joined, "ARTIST=Ariana Grande") {
		t.Error("the artist was lost")
	}
	if !strings.Contains(joined, "GENRE=Pop") {
		t.Error("the new genre is missing")
	}
	if !strings.Contains(joined, "TITLE=Touch It") || strings.Contains(joined, "TITLE=Touch It (Instrumental)") {
		t.Errorf("the title was not replaced: %v", after)
	}
}

// TestPatchOpusKeepsAudioIntact guards the thing that must never break: the
// audio pages have to come through byte for byte.
func TestPatchOpusKeepsAudioIntact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.opus")
	writeTestOpus(t, path, []string{"TITLE=Old"}, 4)

	audioBefore := audioBodies(t, path)

	if err := PatchTags(path, map[string]string{PatchGenre: "Jazz"}); err != nil {
		t.Fatalf("PatchTags: %v", err)
	}

	if audioAfter := audioBodies(t, path); !bytes.Equal(audioBefore, audioAfter) {
		t.Error("audio data changed; retagging must never touch the audio")
	}
}

func audioBodies(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	pages, err := parseOggPages(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, _, last, err := readOpusTags(pages)
	if err != nil {
		t.Fatalf("tags: %v", err)
	}

	var out []byte
	for _, p := range pages[last+1:] {
		out = append(out, p.Body...)
	}
	return out
}

// TestPatchOpusRenumbersPages covers the consequence of the comment packet
// changing size: page sequence numbers must stay contiguous or players reject
// the stream.
func TestPatchOpusRenumbersPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.opus")
	writeTestOpus(t, path, []string{"TITLE=x"}, 3)

	// Grow the comments well past one page so the page count changes.
	if err := PatchTags(path, map[string]string{
		PatchGenre: strings.Repeat("Long Genre Name; ", 6000),
	}); err != nil {
		t.Fatalf("PatchTags: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	pages, err := parseOggPages(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	for i, p := range pages {
		if p.Sequence != uint32(i) {
			t.Fatalf("page %d has sequence %d; numbering must be contiguous", i, p.Sequence)
		}
	}
	if len(pages) < 4 {
		t.Errorf("expected the comment packet to span extra pages, got %d pages", len(pages))
	}
}

// TestOggChecksumsAreValid confirms the rewritten stream verifies, since a bad
// CRC makes a file unplayable rather than merely mistagged.
func TestOggChecksumsAreValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.opus")
	writeTestOpus(t, path, []string{"TITLE=x", "ARTIST=y"}, 2)

	if err := PatchTags(path, map[string]string{PatchGenre: "Rock"}); err != nil {
		t.Fatalf("PatchTags: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	for offset := 0; offset < len(data); {
		segCount := int(data[offset+26])
		tableEnd := offset + oggHeaderFixed + segCount
		bodyLen := 0
		for _, s := range data[offset+oggHeaderFixed : tableEnd] {
			bodyLen += int(s)
		}
		bodyEnd := tableEnd + bodyLen

		stored := binary.LittleEndian.Uint32(data[offset+22 : offset+26])
		header := append([]byte(nil), data[offset:tableEnd]...)
		binary.LittleEndian.PutUint32(header[22:26], 0)

		if got := oggCRCContinue(oggCRC(header), data[tableEnd:bodyEnd]); got != stored {
			t.Fatalf("page at %d has checksum %08x, computed %08x", offset, stored, got)
		}
		offset = bodyEnd
	}
}

func TestReadOpusTagsReportsValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.opus")
	writeTestOpus(t, path, []string{
		"TITLE=Sahra", "GENRE=World Music", "DATE=1996", "TRACKNUMBER=3",
	}, 1)

	got, err := ReadTags(path)
	if err != nil {
		t.Fatalf("ReadTags: %v", err)
	}
	for field, want := range map[string]string{
		PatchTitle: "Sahra", PatchGenre: "World Music",
		PatchYear: "1996", PatchTrackNumber: "3",
	} {
		if got[field] != want {
			t.Errorf("%s = %q, want %q", field, got[field], want)
		}
	}
}
