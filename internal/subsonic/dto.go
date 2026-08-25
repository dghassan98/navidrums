package subsonic

import (
	"encoding/json"
	"strings"
)

// apiEnvelope is the wrapper every Subsonic response carries. Failures arrive
// with HTTP 200 and an error object inside, so the envelope is always checked.
type apiEnvelope struct {
	Response struct {
		Status        string `json:"status"`
		Version       string `json:"version"`
		Type          string `json:"type"`
		ServerVersion string `json:"serverVersion"`
		OpenSubsonic  bool   `json:"openSubsonic"`
		Error         *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		ScanStatus struct {
			Scanning bool `json:"scanning"`
			Count    int  `json:"count"`
		} `json:"scanStatus"`
		SearchResult3 struct {
			Song []songDTO `json:"song"`
		} `json:"searchResult3"`
	} `json:"subsonic-response"`
}

type songDTO struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Artist   string     `json:"artist"`
	Album    string     `json:"album"`
	Suffix   string     `json:"suffix"`
	Path     string     `json:"path"`
	ISRC     stringList `json:"isrc"`
	Year     int        `json:"year"`
	Duration int        `json:"duration"`
	BitRate  int        `json:"bitRate"`
	BitDepth int        `json:"bitDepth"`
}

func (s *songDTO) toSong() Song {
	return Song{
		ID:       s.ID,
		Title:    s.Title,
		Artist:   s.Artist,
		Album:    s.Album,
		ISRC:     s.ISRC.first(),
		Suffix:   strings.ToLower(s.Suffix),
		Path:     s.Path,
		Year:     s.Year,
		Duration: s.Duration,
		BitRate:  s.BitRate,
		BitDepth: s.BitDepth,
		Lossless: isLossless(s.Suffix),
	}
}

// losslessFormats are the container formats that imply no generation loss.
// Anything else is treated as lossy, which is the conservative reading: a file
// wrongly called lossy just stays a re-download candidate.
var losslessFormats = map[string]bool{
	"flac": true,
	"alac": true,
	"wav":  true,
	"aiff": true,
	"ape":  true,
	"wv":   true,
}

func isLossless(suffix string) bool {
	return losslessFormats[strings.ToLower(strings.TrimSpace(suffix))]
}

// stringList handles a field Navidrome sends as an array but other servers may
// send as a bare string. isrc is the one that matters here.
type stringList []string

func (l *stringList) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}

	if data[0] == '[' {
		var items []string
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		*l = items
		return nil
	}

	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	if single != "" {
		*l = []string{single}
	}
	return nil
}

func (l stringList) first() string {
	for _, v := range l {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
