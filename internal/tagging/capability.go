package tagging

import (
	"os/exec"
	"sort"
	"strings"

	"github.com/cesargomez89/navidrums/internal/ffmpeg"
)

// FormatSupport reports whether one container can be retagged in place, and by
// what route.
type FormatSupport struct {
	Extension string
	Route     string
	Available bool
	Reason    string
}

// Capabilities reports what this deployment can actually retag.
//
// Worth reporting rather than assuming: whether a file can be edited depends on
// what is installed in the running container, and discovering a gap while
// halfway through writing to a library is far worse than seeing it beforehand.
func Capabilities() []FormatSupport {
	ffmpegOK, ffmpegWhy := ffmpegAvailable()
	ffprobeOK, ffprobeWhy := binaryAvailable("ffprobe")

	native := []string{".flac", ".mp3", ".opus", ".ogg"}
	viaFFmpeg := []string{".m4a", ".mp4", ".aac"}

	out := make([]FormatSupport, 0, len(native)+len(viaFFmpeg))
	for _, ext := range native {
		out = append(out, FormatSupport{
			Extension: ext, Route: "built in", Available: true,
		})
	}
	for _, ext := range viaFFmpeg {
		support := FormatSupport{Extension: ext, Route: "ffmpeg", Available: ffmpegOK}
		if !ffmpegOK {
			support.Reason = ffmpegWhy
		}
		out = append(out, support)
	}

	// Reading back what was written is how a bad write is caught, so a missing
	// ffprobe is worth surfacing even though it blocks nothing on its own.
	if !ffprobeOK {
		out = append(out, FormatSupport{
			Extension: "(verification)", Route: "ffprobe",
			Available: false, Reason: ffprobeWhy,
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Extension < out[j].Extension })
	return out
}

func ffmpegAvailable() (bool, string) {
	return binaryAvailable(ffmpeg.Binary())
}

func binaryAvailable(name string) (bool, string) {
	if name == "" {
		return false, "no executable configured"
	}
	if strings.ContainsAny(name, `/\`) {
		// An explicit path: check it directly rather than searching PATH.
		if _, err := exec.LookPath(name); err != nil {
			return false, "not found at " + name
		}
		return true, ""
	}
	if _, err := exec.LookPath(name); err != nil {
		return false, name + " is not on PATH in this container"
	}
	return true, ""
}

// CanPatch reports whether a given file could be retagged here.
func CanPatch(path string) (bool, string) {
	ext := strings.ToLower(path)
	if idx := strings.LastIndexByte(ext, '.'); idx >= 0 {
		ext = ext[idx:]
	}

	for _, support := range Capabilities() {
		if support.Extension == ext {
			return support.Available, support.Reason
		}
	}
	return false, "no way to edit " + ext + " without rewriting every tag"
}
