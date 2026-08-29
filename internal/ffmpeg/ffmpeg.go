package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Metadata struct {
	Custom       map[string]string
	Lyrics       string
	Title        string
	Album        string
	Genre        string
	Mood         string
	Language     string
	Composer     string
	Copyright    string
	CoverMime    string
	AlbumArtists []string
	CoverArt     []byte
	Artists      []string
	Year         int
	TrackTotal   int
	DiscNum      int
	DiscTotal    int
	BPM          int
	TrackNum     int
}

var (
	ffmpegBin = "ffmpeg"
)

func SetFFmpegPath(path string) {
	if path != "" {
		ffmpegBin = path
	}
}

func WriteTags(ctx context.Context, inputPath string, meta *Metadata) (string, error) {
	coverPath := ""
	if len(meta.CoverArt) > 0 {
		tmpCover, err := writeTempCover(meta.CoverArt)
		if err == nil {
			coverPath = tmpCover
			defer func() { _ = os.Remove(tmpCover) }()
		}
	}

	args := buildArgs(inputPath, meta, coverPath)

	// #nosec G204 - variable used to specify ffmpeg binary path from config
	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg failed: %w, output: %s", err, string(output))
	}

	tempPath := inputPath + ".mp4"
	return tempPath, nil
}

func buildArgs(inputPath string, meta *Metadata, coverPath string) []string {
	var args []string

	tempPath := inputPath + ".mp4"

	args = append(args, "-y")
	args = append(args, "-i", inputPath)
	if coverPath != "" {
		args = append(args, "-i", coverPath)
	}

	args = append(args, "-map_metadata", "-1")

	if coverPath != "" {
		args = append(args, "-map", "0", "-map", "1", "-c:a", "copy", "-c:v", "copy", "-disposition:v:0", "attached_pic")
	} else {
		args = append(args, "-map", "0", "-c:a", "copy")
	}
	args = append(args, "-f", "mp4")
	args = append(args, "-brand", "M4A ")
	args = append(args, "-movflags", "+faststart")

	if meta.Title != "" {
		args = append(args, "-metadata", fmt.Sprintf("title=%s", meta.Title))
	}
	if len(meta.Artists) > 0 {
		args = append(args, "-metadata", fmt.Sprintf("artist=%s", joinArtists(meta.Artists)))
	}
	if meta.Album != "" {
		args = append(args, "-metadata", fmt.Sprintf("album=%s", meta.Album))
	}
	if len(meta.AlbumArtists) > 0 {
		args = append(args, "-metadata", fmt.Sprintf("album_artist=%s", joinArtists(meta.AlbumArtists)))
	}
	if meta.TrackNum > 0 {
		trackStr := fmt.Sprintf("%d", meta.TrackNum)
		if meta.TrackTotal > 0 {
			trackStr = fmt.Sprintf("%d/%d", meta.TrackNum, meta.TrackTotal)
		}
		args = append(args, "-metadata", fmt.Sprintf("track=%s", trackStr))
	}
	if meta.DiscNum > 0 {
		discStr := fmt.Sprintf("%d", meta.DiscNum)
		if meta.DiscTotal > 0 {
			discStr = fmt.Sprintf("%d/%d", meta.DiscNum, meta.DiscTotal)
		}
		args = append(args, "-metadata", fmt.Sprintf("disc=%s", discStr))
	}
	if meta.Year > 0 {
		args = append(args, "-metadata", fmt.Sprintf("date=%d", meta.Year))
	}
	if meta.Genre != "" {
		args = append(args, "-metadata", fmt.Sprintf("genre=%s", meta.Genre))
	}
	if meta.Composer != "" {
		args = append(args, "-metadata", fmt.Sprintf("composer=%s", meta.Composer))
	}
	if meta.Copyright != "" {
		args = append(args, "-metadata", fmt.Sprintf("copyright=%s", meta.Copyright))
	}
	if meta.BPM > 0 {
		args = append(args, "-metadata", fmt.Sprintf("BPM=%d", meta.BPM))
	}
	if meta.Lyrics != "" {
		args = append(args, "-metadata", fmt.Sprintf("lyrics=%s", meta.Lyrics))
	}
	if meta.Language != "" {
		args = append(args, "-metadata", fmt.Sprintf("language=%s", meta.Language))
	}
	for k, v := range meta.Custom {
		args = append(args, "-metadata", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, tempPath)

	return args
}

func joinArtists(artists []string) string {
	result := ""
	for i, a := range artists {
		if i > 0 {
			result += ", "
		}
		result += a
	}
	return result
}

func writeTempCover(data []byte) (string, error) {
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, "navidrums_cover_"+fmt.Sprintf("%d", time.Now().UnixNano())+".jpg")

	err := os.WriteFile(tmpFile, data, 0600)
	if err != nil {
		return "", err
	}

	return tmpFile, nil
}

func ConvertToFLAC(ctx context.Context, inputPath string) (string, error) {
	ext := filepath.Ext(inputPath)
	outputPath := inputPath[:len(inputPath)-len(ext)] + ".flac"

	args := []string{
		"-y",
		"-i", inputPath,
		"-map", "0:a",
		"-c:a", "flac",
		"-compression_level", "8",
		outputPath,
	}

	// #nosec G204 - variable used to specify ffmpeg binary path from config
	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg conversion to FLAC failed: %w, output: %s", err, string(output))
	}

	return outputPath, nil
}

// PatchMetadata changes only the named tags on a file, carrying every other
// tag and the audio itself across untouched.
//
// This exists for the formats with no pure-Go tag writer — Opus and M4A.
// Nothing is re-encoded: "-c copy" moves the streams across bit for bit, so an
// Opus file is not degraded by being retagged. Converting such files to another
// format to make them taggable would mean lossy-to-lossy re-encoding, which
// costs real audio quality to solve a metadata problem.
//
// The critical difference from WriteTags is "-map_metadata 0" instead of "-1":
// existing metadata is inherited and only the named keys are overridden,
// rather than everything being discarded and rebuilt.
func PatchMetadata(ctx context.Context, inputPath string, changes map[string]string) error {
	if len(changes) == 0 {
		return nil
	}

	ext := filepath.Ext(inputPath)
	temp := inputPath + ".navidrums.patch" + ext

	args := []string{
		"-nostdin", "-y",
		"-i", inputPath,
		"-map", "0",
		"-c", "copy",
		"-map_metadata", "0",
	}

	// Sorted so the command is deterministic, which makes a failure
	// reproducible from the logs.
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-metadata", fmt.Sprintf("%s=%s", k, changes[k]))
	}
	args = append(args, temp)

	cmd := exec.CommandContext(ctx, ffmpegBin, args...) //nolint:gosec // path is configured, args are built here
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("ffmpeg could not patch metadata: %w: %s",
			err, lastLine(stderr.String()))
	}

	if err := os.Rename(temp, inputPath); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("could not replace the patched file: %w", err)
	}
	return nil
}

// lastLine keeps the useful end of ffmpeg's output for an error message.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// Binary reports the ffmpeg executable in use, so callers can check for it
// before promising a file can be retagged.
func Binary() string { return ffmpegBin }
