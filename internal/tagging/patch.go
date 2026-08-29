package tagging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bogem/id3v2/v2"
	"github.com/go-flac/flacvorbis"
	"github.com/go-flac/go-flac"

	"github.com/cesargomez89/navidrums/internal/ffmpeg"
)

// ErrPatchUnsupported reports a format this package cannot edit in place.
//
// It is returned rather than falling back to a full rewrite on purpose: the
// existing taggers replace every tag with the fields Navidrums models, which is
// right for a file it just downloaded and catastrophic for someone's library.
var ErrPatchUnsupported = errors.New("this format cannot be edited without rewriting every tag")

// ErrFFmpegMissing reports that a format needing ffmpeg was given to a system
// without it, rather than silently leaving those files unfixed.
var ErrFFmpegMissing = errors.New("ffmpeg is required to retag this format but was not found")

// ffmpegNames maps a patch field onto the metadata key ffmpeg uses. These are
// the generic names ffmpeg translates into each container's own convention.
var ffmpegNames = map[string]string{
	PatchTitle:       "title",
	PatchISRC:        "ISRC",
	PatchGenre:       "genre",
	PatchYear:        "date",
	PatchTrackNumber: "track",
	PatchDiscNumber:  "disc",
}

// Patch fields, named the way the cleanup names them.
const (
	PatchTitle       = "title"
	PatchISRC        = "isrc"
	PatchGenre       = "genre"
	PatchYear        = "year"
	PatchTrackNumber = "track_number"
	PatchDiscNumber  = "disc_number"
)

// vorbisNames maps a patch field onto its Vorbis comment key.
var vorbisNames = map[string]string{
	PatchTitle:       "TITLE",
	PatchISRC:        "ISRC",
	PatchGenre:       "GENRE",
	PatchYear:        "DATE",
	PatchTrackNumber: "TRACKNUMBER",
	PatchDiscNumber:  "DISCNUMBER",
}

// id3Names maps a patch field onto its ID3v2 frame id.
var id3Names = map[string]string{
	PatchTitle:       "TIT2",
	PatchISRC:        "TSRC",
	PatchGenre:       "TCON",
	PatchYear:        "TDRC",
	PatchTrackNumber: "TRCK",
	PatchDiscNumber:  "TPOS",
}

// ReadTags returns every tag currently on a file, keyed by patch field name.
//
// Only the fields the cleanup can change are reported. The point is to record
// what a value was before it is replaced, so a change can be undone.
func ReadTags(path string) (map[string]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return readFLACTags(path)
	case ".mp3":
		return readMP3Tags(path)
	case ".opus", ".ogg":
		return readOpusTagFile(path)
	case ".m4a", ".mp4", ".aac":
		// Reading these needs ffprobe; the applier records the previous value
		// from the library index instead, which is where it came from.
		return nil, fmt.Errorf("%w: %s", ErrPatchUnsupported, filepath.Ext(path))
	default:
		return nil, fmt.Errorf("%w: %s", ErrPatchUnsupported, filepath.Ext(path))
	}
}

// PatchTags changes only the named fields and leaves every other tag alone.
//
// This is the difference that matters for a library that is not ours: comments,
// lyrics, ReplayGain, groupings and anything else present survive untouched,
// because the existing comment block is edited rather than rebuilt.
func PatchTags(path string, changes map[string]string) error {
	if len(changes) == 0 {
		return nil
	}

	for field := range changes {
		if _, ok := vorbisNames[field]; !ok {
			return fmt.Errorf("unknown tag field %q", field)
		}
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return patchFLAC(path, changes)
	case ".mp3":
		return patchMP3(path, changes)
	case ".opus", ".ogg":
		// Handled directly rather than through ffmpeg, which cannot write an
		// embedded picture back into Ogg and would silently discard the
		// artwork.
		return patchOpus(path, changes)
	case ".m4a", ".mp4", ".aac":
		// No pure-Go writer for these. ffmpeg copies the streams across bit for
		// bit, so nothing is re-encoded and no quality is lost.
		return patchViaFFmpeg(path, changes)
	default:
		return fmt.Errorf("%w: %s", ErrPatchUnsupported, filepath.Ext(path))
	}
}

func patchViaFFmpeg(path string, changes map[string]string) error {
	if _, err := exec.LookPath(ffmpeg.Binary()); err != nil {
		return fmt.Errorf("%w (%s)", ErrFFmpegMissing, filepath.Ext(path))
	}

	mapped := make(map[string]string, len(changes))
	for field, value := range changes {
		if strings.TrimSpace(value) == "" {
			continue
		}
		mapped[ffmpegNames[field]] = value
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Refuse rather than quietly discard artwork. ffmpeg cannot write an
	// embedded picture back into an Ogg or Opus file, so retagging one would
	// return a valid file that has silently lost its cover.
	hasPicture, probeErr := ffmpeg.HasNonAudioStream(ctx, path)
	if probeErr == nil && hasPicture {
		return fmt.Errorf("%w: %s", ffmpeg.ErrAttachedPicture, filepath.Base(path))
	}

	return ffmpeg.PatchMetadata(ctx, path, mapped)
}

// ── FLAC ─────────────────────────────────────────────────────────────────────

func readFLACTags(path string) (map[string]string, error) {
	f, err := flac.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read FLAC: %w", err)
	}

	out := map[string]string{}
	for _, block := range f.Meta {
		if block.Type != flac.VorbisComment {
			continue
		}
		vc, err := flacvorbis.ParseFromMetaDataBlock(*block)
		if err != nil {
			return nil, fmt.Errorf("could not read FLAC comments: %w", err)
		}
		for field, key := range vorbisNames {
			if values, err := vc.Get(key); err == nil && len(values) > 0 {
				out[field] = values[0]
			}
		}
	}
	return out, nil
}

func patchFLAC(path string, changes map[string]string) error {
	f, err := flac.ParseFile(path)
	if err != nil {
		return fmt.Errorf("could not read FLAC: %w", err)
	}

	index := -1
	var existing *flacvorbis.MetaDataBlockVorbisComment
	for i, block := range f.Meta {
		if block.Type != flac.VorbisComment {
			continue
		}
		parsed, parseErr := flacvorbis.ParseFromMetaDataBlock(*block)
		if parseErr != nil {
			return fmt.Errorf("could not read FLAC comments: %w", parseErr)
		}
		index, existing = i, parsed
		break
	}

	if existing == nil {
		existing = flacvorbis.New()
	}

	// Rebuild the comment list, dropping only the keys being changed and
	// carrying every other comment across exactly as it was.
	replacing := make(map[string]bool, len(changes))
	for field := range changes {
		replacing[strings.ToUpper(vorbisNames[field])] = true
	}

	kept := make([]string, 0, len(existing.Comments))
	for _, comment := range existing.Comments {
		key := strings.ToUpper(comment)
		if idx := strings.IndexByte(comment, '='); idx >= 0 {
			key = strings.ToUpper(comment[:idx])
		}
		if !replacing[key] {
			kept = append(kept, comment)
		}
	}

	for field, value := range changes {
		if strings.TrimSpace(value) == "" {
			continue
		}
		kept = append(kept, vorbisNames[field]+"="+value)
	}

	existing.Comments = kept
	block := existing.Marshal()

	if index >= 0 {
		f.Meta[index] = &block
	} else {
		f.Meta = append(f.Meta, &block)
	}

	return saveFLACAtomically(f, path)
}

// saveFLACAtomically writes to a temporary file beside the original and renames
// it into place, so an interrupted write cannot leave a half-written file where
// the music used to be.
//
// Creating that temporary file needs write permission on the *directory*, which
// a library may not grant even when the files themselves are writable — that is
// why MP3s succeed while FLACs fail with "permission denied" in the same tree.
// When the directory refuses, the content is staged outside and written over the
// original instead, which needs only the file to be writable.
func saveFLACAtomically(f *flac.File, path string) error {
	temp := path + ".navidrums.tmp"

	err := f.Save(temp)
	if err == nil {
		if renameErr := os.Rename(temp, path); renameErr != nil {
			_ = os.Remove(temp)
			return fmt.Errorf("could not replace FLAC: %w", renameErr)
		}
		return nil
	}

	_ = os.Remove(temp)
	if !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("could not write FLAC: %w", err)
	}

	return saveFLACInPlace(f, path)
}

// saveFLACInPlace stages the file somewhere writable and copies it over the
// original.
//
// This gives up the atomicity of a rename: the original is truncated before the
// new content lands, so an interruption here damages the file. It is the only
// way to write into a directory that cannot be added to, and it is used only
// when the atomic path has already been refused.
func saveFLACInPlace(f *flac.File, path string) error {
	staged, err := os.CreateTemp("", "navidrums-*.flac")
	if err != nil {
		return fmt.Errorf("could not stage FLAC: %w", err)
	}
	stagedName := staged.Name()
	_ = staged.Close()
	defer func() { _ = os.Remove(stagedName) }()

	if err := f.Save(stagedName); err != nil {
		return fmt.Errorf("could not write FLAC: %w", err)
	}

	content, err := os.ReadFile(stagedName) //nolint:gosec // path is ours
	if err != nil {
		return fmt.Errorf("could not read the staged FLAC: %w", err)
	}

	// Opened without O_CREATE: this must overwrite the existing file, never
	// create a new one, since creating is exactly what was refused.
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0) //nolint:gosec // caller supplies the library path
	if err != nil {
		// Say who owns it and who we are. Otherwise diagnosing this means
		// running stat by hand inside the container for every failure.
		return fmt.Errorf("cannot write this file: %w (%s)", err, ownershipHint(path))
	}
	defer func() { _ = dst.Close() }()

	if _, err := dst.Write(content); err != nil {
		return fmt.Errorf("could not overwrite the FLAC: %w", err)
	}
	return dst.Sync()
}

// ownershipHint describes who owns a file and who is trying to write it, so a
// permission failure carries its own diagnosis.
func ownershipHint(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "could not stat it either"
	}

	mode := info.Mode().Perm()
	owner := "unknown"
	if uid, gid, ok := fileOwner(info); ok {
		owner = fmt.Sprintf("owned by %d:%d", uid, gid)
	}

	return fmt.Sprintf("%s, mode %04o; this process runs as %d:%d",
		owner, mode, os.Getuid(), os.Getgid())
}

// writeFileAtomically replaces a file's contents, preferring a temporary file
// and a rename so an interrupted write cannot damage the original.
//
// Creating that temporary file needs write permission on the directory, which a
// library may withhold even where the files themselves are writable. When that
// is refused, the content is written over the original instead, which needs
// only the file — losing atomicity, but it is that or leave the file unfixable.
func writeFileAtomically(path string, content []byte) error {
	temp := path + ".navidrums.tmp"

	err := os.WriteFile(temp, content, 0o644) //nolint:gosec // matches library files
	if err == nil {
		if renameErr := os.Rename(temp, path); renameErr != nil {
			_ = os.Remove(temp)
			return fmt.Errorf("could not replace the file: %w", renameErr)
		}
		return nil
	}
	_ = os.Remove(temp)

	if !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("could not write the file: %w", err)
	}

	// Without O_CREATE: this must overwrite, never create, since creating is
	// what was just refused.
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0) //nolint:gosec // caller supplies the library path
	if err != nil {
		return fmt.Errorf("cannot write this file: %w (%s)", err, ownershipHint(path))
	}
	defer func() { _ = dst.Close() }()

	if _, err := dst.Write(content); err != nil {
		return fmt.Errorf("could not overwrite the file: %w", err)
	}
	return dst.Sync()
}

// ── MP3 ──────────────────────────────────────────────────────────────────────

func readMP3Tags(path string) (map[string]string, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return nil, fmt.Errorf("could not read MP3 tags: %w", err)
	}
	defer func() { _ = tag.Close() }()

	out := map[string]string{}
	for field, frame := range id3Names {
		if value := tag.GetTextFrame(frame).Text; value != "" {
			out[field] = value
		}
	}
	return out, nil
}

func patchMP3(path string, changes map[string]string) error {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return fmt.Errorf("could not read MP3 tags: %w", err)
	}
	defer func() { _ = tag.Close() }()

	// id3v2 keeps every frame it parsed, including ones it does not model, so
	// setting one frame leaves the rest of the tag intact.
	for field, value := range changes {
		if strings.TrimSpace(value) == "" {
			continue
		}
		tag.AddTextFrame(id3Names[field], tag.DefaultEncoding(), value)
	}

	if err := tag.Save(); err != nil {
		return fmt.Errorf("could not write MP3 tags: %w", err)
	}
	return nil
}
