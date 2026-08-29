package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-flac/flacvorbis"
	"github.com/go-flac/go-flac"
)

// writeTestFLACForApply builds a minimal valid FLAC so the applier can be
// exercised against a real file rather than a stub.
func writeTestFLACForApply(t *testing.T, path string, comments map[string]string) {
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

func newApplyService(t *testing.T, mount string, enabled bool) *LibraryApplyService {
	t.Helper()
	return NewLibraryApplyService(nil, mount, "/music", enabled, nil)
}

// TestApplyRefusesWhenWritingIsDisabled is the gate that keeps the library
// read-only by default. It is an environment variable rather than a setting so
// it cannot be left on by accident.
func TestApplyRefusesWhenWritingIsDisabled(t *testing.T) {
	s := newApplyService(t, t.TempDir(), false)

	if _, err := s.Apply(20, false); !errors.Is(err, ErrWriteDisabled) {
		t.Errorf("Apply = %v, want ErrWriteDisabled", err)
	}
}

func TestResolvePathMapsBetweenContainers(t *testing.T) {
	// The music server and Navidrums mount the same directory in different
	// places, so an indexed path cannot be opened directly.
	mount := t.TempDir()
	nested := filepath.Join(mount, "Library", "Khaled")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(nested, "Sahra.flac")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := newApplyService(t, mount, true)

	got, err := s.ResolvePath("/music/Library/Khaled/Sahra.flac")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if got != target {
		t.Errorf("got %q, want %q", got, target)
	}
}

// TestResolvePathFailsLoudlyOnAMissingFile matters because a silent skip would
// make a run look successful while changing nothing.
func TestResolvePathFailsLoudlyOnAMissingFile(t *testing.T) {
	s := newApplyService(t, t.TempDir(), true)

	if _, err := s.ResolvePath("/music/Library/Nope/Missing.flac"); err == nil {
		t.Error("a missing file resolved without error")
	}
}

func TestResolvePathRejectsAnUnexpectedPrefix(t *testing.T) {
	// A path that does not start where expected means the prefix is
	// misconfigured; guessing would write to the wrong file.
	s := newApplyService(t, t.TempDir(), true)

	_, err := s.ResolvePath("/elsewhere/Library/A/B.flac")
	if err == nil {
		t.Fatal("an unexpected prefix was accepted")
	}
	if !strings.Contains(err.Error(), "LIBRARY_SOURCE_PREFIX") {
		t.Errorf("error should name the setting to fix: %v", err)
	}
}

func TestResolvePathNeedsAMount(t *testing.T) {
	s := newApplyService(t, "", true)

	if _, err := s.ResolvePath("/music/a.flac"); !errors.Is(err, ErrNoLibraryMount) {
		t.Errorf("err = %v, want ErrNoLibraryMount", err)
	}
}

func TestStatusExplainsWhyApplyingIsUnavailable(t *testing.T) {
	if _, _, reason := newApplyService(t, "/library", false).Status(); reason == "" {
		t.Error("disabled writing gave no reason")
	}
	if _, _, reason := newApplyService(t, "", true).Status(); reason == "" {
		t.Error("a missing mount gave no reason")
	}
	if ok, _, _ := newApplyService(t, "/library", true).Status(); !ok {
		t.Error("a configured service reported unavailable")
	}
}

// TestVerifyCatchesAWriteThatDidNotTake is the safety net for the formats this
// machine cannot exercise. A write reported as successful but not actually
// present must never be recorded as applied, or the queue would claim work
// that never happened.
func TestVerifyCatchesAWriteThatDidNotTake(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.flac")
	writeTestFLACForApply(t, path, map[string]string{"TITLE": "Old", "GENRE": "Rock"})

	s := newApplyService(t, dir, true)

	// The file genuinely says Rock, so claiming Pop was written must fail.
	if err := s.verify(path, map[string]string{"genre": "Pop"}); err == nil {
		t.Error("verify passed for a value that is not on the file")
	}

	// And the value that is there must pass.
	if err := s.verify(path, map[string]string{"genre": "Rock"}); err != nil {
		t.Errorf("verify failed for a value that is on the file: %v", err)
	}
}
