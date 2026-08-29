package tagging

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Minimal Ogg container handling, enough to replace one packet and re-emit the
// stream. Written here rather than pulled in because the job is narrow: read
// the pages, swap the comment packet, renumber and re-checksum.

const (
	oggMagic       = "OggS"
	oggHeaderFixed = 27  // bytes before the segment table
	oggMaxSegments = 255 // per page
	oggMaxSegment  = 255 // per lacing value

	oggFlagContinued = 0x01
	oggFlagBOS       = 0x02
)

var errNotOgg = errors.New("not an Ogg stream")

type oggPage struct {
	Segments   []byte
	Body       []byte
	Granule    uint64
	Serial     uint32
	Sequence   uint32
	HeaderType byte
}

// packetContinues reports whether this page ends mid-packet, which is the case
// when its final segment is a full 255 bytes.
func (p oggPage) packetContinues() bool {
	return len(p.Segments) > 0 && p.Segments[len(p.Segments)-1] == oggMaxSegment
}

// parseOggPages reads every page in order.
func parseOggPages(data []byte) ([]oggPage, error) {
	var pages []oggPage

	for offset := 0; offset < len(data); {
		if len(data)-offset < oggHeaderFixed {
			return nil, fmt.Errorf("%w: truncated page header", errNotOgg)
		}
		if string(data[offset:offset+4]) != oggMagic {
			return nil, fmt.Errorf("%w: no page marker at byte %d", errNotOgg, offset)
		}

		segmentCount := int(data[offset+26])
		tableEnd := offset + oggHeaderFixed + segmentCount
		if tableEnd > len(data) {
			return nil, fmt.Errorf("%w: truncated segment table", errNotOgg)
		}

		segments := data[offset+oggHeaderFixed : tableEnd]
		bodyLen := 0
		for _, s := range segments {
			bodyLen += int(s)
		}
		bodyEnd := tableEnd + bodyLen
		if bodyEnd > len(data) {
			return nil, fmt.Errorf("%w: truncated page body", errNotOgg)
		}

		pages = append(pages, oggPage{
			HeaderType: data[offset+5],
			Granule:    binary.LittleEndian.Uint64(data[offset+6 : offset+14]),
			Serial:     binary.LittleEndian.Uint32(data[offset+14 : offset+18]),
			Sequence:   binary.LittleEndian.Uint32(data[offset+18 : offset+22]),
			Segments:   append([]byte(nil), segments...),
			Body:       append([]byte(nil), data[tableEnd:bodyEnd]...),
		})
		offset = bodyEnd
	}

	if len(pages) == 0 {
		return nil, fmt.Errorf("%w: no pages found", errNotOgg)
	}
	return pages, nil
}

// buildOggPages splits one packet across as many pages as it needs.
//
// A cover image easily exceeds the 65025 bytes a single page can hold, so
// spanning is the normal case here rather than an edge one.
func buildOggPages(packet []byte, serial uint32, firstSequence uint32, granule uint64) []oggPage {
	// Lacing: full 255s, then a shorter value to end the packet. A packet whose
	// length is an exact multiple of 255 needs a trailing zero, or a reader
	// cannot tell it has finished.
	var lacing []byte
	remaining := len(packet)
	for remaining >= oggMaxSegment {
		lacing = append(lacing, oggMaxSegment)
		remaining -= oggMaxSegment
	}
	lacing = append(lacing, byte(remaining))

	var pages []oggPage
	consumed := 0
	for i := 0; i < len(lacing); i += oggMaxSegments {
		end := i + oggMaxSegments
		if end > len(lacing) {
			end = len(lacing)
		}
		segments := lacing[i:end]

		bodyLen := 0
		for _, s := range segments {
			bodyLen += int(s)
		}

		headerType := byte(0)
		if i > 0 {
			headerType = oggFlagContinued
		}

		pages = append(pages, oggPage{
			HeaderType: headerType,
			Granule:    granule,
			Serial:     serial,
			Sequence:   firstSequence + uint32(len(pages)),
			Segments:   append([]byte(nil), segments...),
			Body:       append([]byte(nil), packet[consumed:consumed+bodyLen]...),
		})
		consumed += bodyLen
	}
	return pages
}

// marshalOggPages writes pages back out, renumbering them so the sequence stays
// contiguous and recomputing every checksum.
//
// Renumbering is not optional: replacing the comment packet can change how many
// pages it occupies, which shifts every page after it.
func marshalOggPages(pages []oggPage) []byte {
	var out []byte

	for i := range pages {
		page := pages[i]
		header := make([]byte, oggHeaderFixed+len(page.Segments))

		copy(header[0:4], oggMagic)
		header[4] = 0 // stream structure version
		header[5] = page.HeaderType
		binary.LittleEndian.PutUint64(header[6:14], page.Granule)
		binary.LittleEndian.PutUint32(header[14:18], page.Serial)
		binary.LittleEndian.PutUint32(header[18:22], uint32(i))
		// Checksum field stays zero while the checksum is computed over it.
		binary.LittleEndian.PutUint32(header[22:26], 0)
		header[26] = byte(len(page.Segments))
		copy(header[oggHeaderFixed:], page.Segments)

		crc := oggCRC(header)
		crc = oggCRCContinue(crc, page.Body)
		binary.LittleEndian.PutUint32(header[22:26], crc)

		out = append(out, header...)
		out = append(out, page.Body...)
	}

	return out
}

// oggCRCTable holds the CRC-32 Ogg uses: polynomial 0x04c11db7, no reflection
// and no final inversion, which is why the standard library cannot supply it.
var oggCRCTable = func() [256]uint32 {
	var table [256]uint32
	for i := range table {
		crc := uint32(i) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
		table[i] = crc
	}
	return table
}()

func oggCRC(data []byte) uint32 { return oggCRCContinue(0, data) }

func oggCRCContinue(crc uint32, data []byte) uint32 {
	for _, b := range data {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}
