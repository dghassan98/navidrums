package tagging

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Opus tagging, done directly rather than through ffmpeg.
//
// Opus stores its tags as Vorbis comments in an OpusTags packet, and cover art
// as a METADATA_BLOCK_PICTURE comment — the same arrangement FLAC uses. ffmpeg
// turns that comment into a video stream when reading and cannot write one back
// into Ogg, so retagging through it silently discards the artwork. Editing the
// comment list in place avoids the problem entirely: the picture is simply a
// comment nobody touched.

const opusTagsMagic = "OpusTags"

var errNoOpusTags = errors.New("no OpusTags packet found")

// opusComments is a parsed comment header.
type opusComments struct {
	Vendor   string
	Comments []string
}

// readOpusTags returns the comment packet and which pages carried it.
func readOpusTags(pages []oggPage) (comments opusComments, first, last int, err error) {
	// The comment packet is the second packet in the stream: page 0 holds
	// OpusHead, and OpusTags starts on the page after it.
	if len(pages) < 2 {
		return comments, 0, 0, errNoOpusTags
	}

	first = 1
	last = first
	packet := append([]byte(nil), pages[first].Body...)
	for pages[last].packetContinues() && last+1 < len(pages) {
		last++
		packet = append(packet, pages[last].Body...)
	}

	if len(packet) < len(opusTagsMagic) || string(packet[:len(opusTagsMagic)]) != opusTagsMagic {
		return comments, 0, 0, errNoOpusTags
	}

	parsed, err := parseOpusComments(packet)
	return parsed, first, last, err
}

func parseOpusComments(packet []byte) (opusComments, error) {
	var out opusComments

	pos := len(opusTagsMagic)
	readUint32 := func() (uint32, bool) {
		if pos+4 > len(packet) {
			return 0, false
		}
		v := binary.LittleEndian.Uint32(packet[pos : pos+4])
		pos += 4
		return v, true
	}

	vendorLen, ok := readUint32()
	if !ok || pos+int(vendorLen) > len(packet) {
		return out, fmt.Errorf("%w: truncated vendor string", errNoOpusTags)
	}
	out.Vendor = string(packet[pos : pos+int(vendorLen)])
	pos += int(vendorLen)

	count, ok := readUint32()
	if !ok {
		return out, fmt.Errorf("%w: truncated comment count", errNoOpusTags)
	}

	for range count {
		length, ok := readUint32()
		if !ok || pos+int(length) > len(packet) {
			return out, fmt.Errorf("%w: truncated comment", errNoOpusTags)
		}
		out.Comments = append(out.Comments, string(packet[pos:pos+int(length)]))
		pos += int(length)
	}

	return out, nil
}

func marshalOpusComments(c opusComments) []byte {
	out := make([]byte, 0, 64)
	out = append(out, opusTagsMagic...)

	out = binary.LittleEndian.AppendUint32(out, uint32(len(c.Vendor)))
	out = append(out, c.Vendor...)

	out = binary.LittleEndian.AppendUint32(out, uint32(len(c.Comments)))
	for _, comment := range c.Comments {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(comment)))
		out = append(out, comment...)
	}

	return out
}

// readOpusTagFile reports the patchable tags currently on an Opus file.
func readOpusTagFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // caller supplies the library path
	if err != nil {
		return nil, err
	}

	pages, err := parseOggPages(data)
	if err != nil {
		return nil, err
	}

	comments, _, _, err := readOpusTags(pages)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}
	for field, key := range vorbisNames {
		for _, comment := range comments.Comments {
			name, value, found := strings.Cut(comment, "=")
			if found && strings.EqualFold(name, key) {
				out[field] = value
				break
			}
		}
	}
	return out, nil
}

// patchOpus replaces the named tags and leaves every other comment alone,
// artwork included.
func patchOpus(path string, changes map[string]string) error {
	data, err := os.ReadFile(path) //nolint:gosec // caller supplies the library path
	if err != nil {
		return err
	}

	pages, err := parseOggPages(data)
	if err != nil {
		return fmt.Errorf("could not read Opus: %w", err)
	}

	comments, first, last, err := readOpusTags(pages)
	if err != nil {
		return fmt.Errorf("could not read Opus tags: %w", err)
	}

	replacing := make(map[string]bool, len(changes))
	for field := range changes {
		replacing[strings.ToUpper(vorbisNames[field])] = true
	}

	kept := make([]string, 0, len(comments.Comments)+len(changes))
	for _, comment := range comments.Comments {
		name, _, _ := strings.Cut(comment, "=")
		if !replacing[strings.ToUpper(name)] {
			// METADATA_BLOCK_PICTURE lands here like any other comment, which
			// is exactly why the artwork survives.
			kept = append(kept, comment)
		}
	}
	for field, value := range changes {
		if strings.TrimSpace(value) == "" {
			continue
		}
		kept = append(kept, vorbisNames[field]+"="+value)
	}
	comments.Comments = kept

	// Rebuild: the header page, the new comment pages, then the audio
	// untouched. marshalOggPages renumbers everything, which matters because
	// the comment packet may now occupy a different number of pages.
	rebuilt := make([]oggPage, 0, len(pages)+2)
	rebuilt = append(rebuilt, pages[0])
	rebuilt = append(rebuilt, buildOggPages(
		marshalOpusComments(comments), pages[first].Serial, 1, 0)...)
	rebuilt = append(rebuilt, pages[last+1:]...)

	return writeFileAtomically(path, marshalOggPages(rebuilt))
}
