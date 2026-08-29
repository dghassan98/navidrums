package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cesargomez89/navidrums/internal/store"
	"github.com/cesargomez89/navidrums/internal/tagging"
)

// ErrWriteDisabled reports that writing to the library was not switched on.
//
// The gate is an environment variable rather than a setting so it cannot be
// left on by accident: turning it off again is a restart, not a click.
var ErrWriteDisabled = errors.New("writing to the library is disabled; set NAVIDRUMS_LIBRARY_WRITE=1 to enable it")

// ErrNoLibraryMount reports that the library is not reachable from here.
var ErrNoLibraryMount = errors.New("the music library is not mounted; set LIBRARY_MOUNT to where it is mounted in this container")

// LibraryApplyService writes approved tag fixes to the files.
//
// Runs are performed in the background. Writing tags means reading each file,
// rewriting it and reading it back, and for some formats shelling out to
// ffmpeg — far too slow to hold an HTTP request open, which is how the first
// version appeared to hang.
type LibraryApplyService struct {
	db           *store.DB
	logger       *slog.Logger
	mount        string
	sourcePrefix string
	enabled      bool

	mu       sync.Mutex
	running  bool
	progress ApplyProgress
	report   *ApplyReport
}

// ApplyProgress is what the panel polls while a run is under way.
type ApplyProgress struct {
	Current   string
	StartedAt time.Time
	Done      int
	Total     int
	Changed   int
	Failed    int
	Running   bool
}

// ErrApplyRunning reports a second run starting while one is in flight.
var ErrApplyRunning = errors.New("a library apply is already running")

// Progress reports the state of the current or last run.
func (s *LibraryApplyService) Progress() (ApplyProgress, *ApplyReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.progress
	p.Running = s.running
	return p, s.report
}

// Start begins a run in the background and returns immediately.
func (s *LibraryApplyService) Start(limit int, dryRun bool) error {
	if !dryRun && !s.enabled {
		return ErrWriteDisabled
	}
	if limit <= 0 {
		limit = 20
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrApplyRunning
	}
	s.running = true
	s.progress = ApplyProgress{StartedAt: time.Now()}
	s.report = nil
	s.mu.Unlock()

	go func() {
		report, err := s.Apply(limit, dryRun)

		s.mu.Lock()
		s.running = false
		if err != nil && report == nil {
			report = &ApplyReport{DryRun: dryRun}
			report.RescanError = err.Error()
		}
		s.report = report
		s.mu.Unlock()

		if err != nil && s.logger != nil {
			s.logger.Error("Library apply failed", "error", err)
		}
	}()

	return nil
}

func NewLibraryApplyService(db *store.DB, mount, sourcePrefix string, enabled bool, logger *slog.Logger) *LibraryApplyService {
	if sourcePrefix == "" {
		sourcePrefix = "/music"
	}
	return &LibraryApplyService{
		db: db, logger: logger,
		mount:        strings.TrimRight(mount, `/\`),
		sourcePrefix: strings.TrimRight(sourcePrefix, `/\`),
		enabled:      enabled,
	}
}

// ApplyOutcome is what happened to one file.
type ApplyOutcome struct {
	NavidromeID string
	Title       string
	Path        string
	Error       string
	Applied     []string
	Skipped     bool
}

// ApplyReport summarises a run.
type ApplyReport struct {
	Outcomes    []ApplyOutcome
	RescanError string
	Rescanned   bool
	DryRun      bool
	Files       int
	Changed     int
	Failed      int
	FieldsSet   int
}

// ResolvePath maps a path as the music server reports it onto this container's
// view of the same file.
//
// The two containers mount the same directory at different places, so a path
// from the index cannot be opened directly. A file that does not resolve is an
// error rather than a skip: silently passing over files would make a run look
// successful while doing nothing.
func (s *LibraryApplyService) ResolvePath(reported string) (string, error) {
	if s.mount == "" {
		return "", ErrNoLibraryMount
	}

	clean := strings.ReplaceAll(reported, `\`, "/")
	prefix := strings.ReplaceAll(s.sourcePrefix, `\`, "/")

	if !strings.HasPrefix(clean, prefix+"/") {
		return "", fmt.Errorf("path %q does not start with %q; check LIBRARY_SOURCE_PREFIX", reported, prefix)
	}

	local := filepath.Join(s.mount, filepath.FromSlash(strings.TrimPrefix(clean, prefix+"/")))
	if _, err := os.Stat(local); err != nil {
		return "", fmt.Errorf("resolved to %q which is not readable: %w", local, err)
	}
	return local, nil
}

// Apply writes up to limit files' approved changes.
//
// dryRun does everything except the write itself — resolving paths, checking
// the format can be edited, reading current values — so a run can be inspected
// before anything is touched.
func (s *LibraryApplyService) Apply(limit int, dryRun bool) (*ApplyReport, error) {
	if !dryRun && !s.enabled {
		return nil, ErrWriteDisabled
	}
	if limit <= 0 {
		limit = 20
	}

	files, err := s.db.ApprovedFixFiles(limit)
	if err != nil {
		return nil, err
	}

	report := &ApplyReport{DryRun: dryRun, Files: len(files)}

	s.mu.Lock()
	s.progress.Total = len(files)
	s.mu.Unlock()

	for i := range files {
		s.mu.Lock()
		s.progress.Current = files[i].Title
		s.mu.Unlock()

		outcome := s.applyOne(files[i], dryRun)
		report.Outcomes = append(report.Outcomes, outcome)

		switch {
		case outcome.Error != "":
			report.Failed++
		case len(outcome.Applied) > 0:
			report.Changed++
			report.FieldsSet += len(outcome.Applied)
		}

		s.mu.Lock()
		s.progress.Done = i + 1
		s.progress.Changed = report.Changed
		s.progress.Failed = report.Failed
		s.mu.Unlock()
	}

	if s.logger != nil {
		s.logger.Info("Library apply finished",
			"dry_run", dryRun, "files", report.Files,
			"changed", report.Changed, "failed", report.Failed)
	}
	return report, nil
}

func (s *LibraryApplyService) applyOne(file store.FixFile, dryRun bool) ApplyOutcome {
	out := ApplyOutcome{NavidromeID: file.NavidromeID, Title: file.Title, Path: file.Path}

	local, err := s.ResolvePath(file.Path)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.Path = local

	if ok, reason := tagging.CanPatch(local); !ok {
		out.Error = reason
		return out
	}

	changes := make(map[string]string, len(file.Fixes))
	for _, fix := range file.Fixes {
		changes[fix.Field] = fix.ProposedValue
	}

	// Read what is on the file now, not what the index believed, so an undo
	// restores the real previous value.
	previous, readErr := tagging.ReadTags(local)
	if readErr != nil {
		// Formats read through ffprobe are not readable here; fall back to the
		// indexed value rather than refusing to proceed.
		previous = map[string]string{}
		for _, fix := range file.Fixes {
			previous[fix.Field] = fix.CurrentValue
		}
	}

	if dryRun {
		for field := range changes {
			out.Applied = append(out.Applied, field)
		}
		out.Skipped = true
		return out
	}

	if err := tagging.PatchTags(local, changes); err != nil {
		out.Error = err.Error()
		return out
	}

	// Read back and confirm. A write that silently did not take must not be
	// recorded as applied, or the queue would claim work that never happened.
	if verifyErr := s.verify(local, changes); verifyErr != nil {
		out.Error = verifyErr.Error()
		return out
	}

	if err := s.db.RecordApplied(file.NavidromeID, local, previous, changes); err != nil {
		out.Error = "written but not recorded: " + err.Error()
		return out
	}

	for field := range changes {
		out.Applied = append(out.Applied, field)
	}
	return out
}

// verify re-reads the file and checks the new values are there.
func (s *LibraryApplyService) verify(path string, changes map[string]string) error {
	after, err := tagging.ReadTags(path)
	if err != nil {
		// Nothing can be read back for this format; the write reported success
		// and there is no stronger claim available.
		return nil
	}

	for field, want := range changes {
		if got := strings.TrimSpace(after[field]); !strings.EqualFold(got, strings.TrimSpace(want)) {
			return fmt.Errorf("wrote %s=%q but the file reads back %q", field, want, got)
		}
	}
	return nil
}

// Status describes whether applying is possible at all.
func (s *LibraryApplyService) Status() (enabled bool, mount string, reason string) {
	switch {
	case !s.enabled:
		return false, s.mount, ErrWriteDisabled.Error()
	case s.mount == "":
		return false, "", ErrNoLibraryMount.Error()
	default:
		return true, s.mount, ""
	}
}
