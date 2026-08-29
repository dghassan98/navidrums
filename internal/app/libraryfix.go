package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/store"
)

// Fields the cleanup is allowed to propose.
//
// Artist, album and album artist are deliberately absent: they are populated on
// every track in the library, and rewriting a correct name from a fuzzy match is
// how a cleanup damages a collection rather than improving it.
//
// Title is included, but never applies unattended — see AlwaysReview. Part of
// the library is ripped from video, carrying artefacts like "(EDIT 4K)" in the
// title, while other titles carry the owner's own annotations such as
// "(Instrumental)". Those are the same shape and want opposite outcomes, so no
// rule can separate them and a person decides every one.
const (
	FieldTitle       = "title"
	FieldISRC        = "isrc"
	FieldGenre       = "genre"
	FieldYear        = "year"
	FieldTrackNumber = "track_number"
	FieldDiscNumber  = "disc_number"
)

// LibraryFixService generates proposed tag fixes by comparing the library
// index against the catalog. It never writes to a file: the output is a
// reviewable record of what a cleanup would do.
type LibraryFixService struct {
	// Resolved per use rather than held: credentials can change in Settings,
	// which rebuilds the provider. A pointer captured at startup would keep
	// searching with the old one.
	catalog func() CatalogSearcher
	db      *store.DB
	logger  *slog.Logger

	mu       sync.Mutex
	running  bool
	progress FixProgress
}

// CatalogSearcher is the slice of the catalog provider this needs, kept narrow
// so the dry run can be tested without a provider.
type CatalogSearcher interface {
	Search(ctx context.Context, query string, searchType string) (*domain.SearchResult, error)
}

type FixProgress struct {
	StartedAt time.Time
	Finished  time.Time
	LastError string
	Scanned   int
	Total     int
	Matched   int
	Proposals int
	Running   bool
}

func NewLibraryFixService(catalog func() CatalogSearcher, db *store.DB, logger *slog.Logger) *LibraryFixService {
	return &LibraryFixService{catalog: catalog, db: db, logger: logger}
}

func (s *LibraryFixService) Progress() FixProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.progress
	p.Running = s.running
	return p
}

// ErrDryRunRunning reports a second dry run starting while one is in flight.
var ErrDryRunRunning = errors.New("a library dry run is already running")

// StartDryRun scans the library in the background and records what it would
// change. It returns as soon as the scan is under way.
//
// It runs in the background because a full pass is thousands of catalog
// lookups against a throttled client — tens of minutes, far past any request.
// StartDryRun scans in the background. full re-examines the whole library;
// otherwise only tracks never scanned before are looked at, which is what
// makes this usable after each download rather than a 27 minute event.
func (s *LibraryFixService) StartDryRun(ctx context.Context, full bool, done func()) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrDryRunRunning
	}
	s.running = true
	s.progress = FixProgress{StartedAt: time.Now()}
	s.mu.Unlock()

	go func() {
		if done != nil {
			defer done()
		}
		err := s.dryRun(ctx, full)

		s.mu.Lock()
		s.running = false
		s.progress.Finished = time.Now()
		if err != nil {
			s.progress.LastError = err.Error()
		}
		s.mu.Unlock()

		if err != nil && s.logger != nil {
			s.logger.Error("Library dry run failed", "error", err)
		}
	}()

	return nil
}

func (s *LibraryFixService) dryRun(ctx context.Context, full bool) error {
	if full {
		// A full run re-examines everything, so what was scanned before no
		// longer limits it.
		if err := s.db.ClearScanState(); err != nil {
			return fmt.Errorf("could not reset the scan state: %w", err)
		}
	}

	tracks, err := s.db.UnscannedLibraryTracks()
	if err != nil {
		return fmt.Errorf("could not read the library index: %w", err)
	}

	s.mu.Lock()
	s.progress.Total = len(tracks)
	s.mu.Unlock()

	fixes := make([]store.LibraryFix, 0, len(tracks))
	scanned := make([]string, 0, len(tracks))

	for i := range tracks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		scanned = append(scanned, tracks[i].NavidromeID)

		match, confidence := s.findCatalogTrack(ctx, tracks[i])

		s.mu.Lock()
		s.progress.Scanned++
		if match != nil {
			s.progress.Matched++
		}
		s.mu.Unlock()

		if match == nil {
			continue
		}

		found := ProposeFixes(tracks[i], *match, confidence)
		fixes = append(fixes, found...)

		s.mu.Lock()
		s.progress.Proposals += len(found)
		s.mu.Unlock()
	}

	// Appended rather than replacing the set: an incremental run has not
	// looked at the rest of the library, so replacing would silently discard
	// every proposal it did not examine.
	if err := s.db.AppendLibraryFixes(fixes); err != nil {
		return fmt.Errorf("could not record proposals: %w", err)
	}

	// Marked only after the proposals are safely stored, so an interrupted run
	// re-examines those tracks next time rather than skipping them forever.
	if err := s.db.MarkScanned(scanned); err != nil {
		return fmt.Errorf("could not record scan progress: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("Library dry run finished",
			"scanned", len(tracks), "proposals", len(fixes), "full", full)
	}
	return nil
}

// findCatalogTrack locates the catalog counterpart of a library track.
//
// An ISRC match is exact and is what makes a proposal safe to apply
// unattended. Everything else is a name match and is only ever a suggestion.
func (s *LibraryFixService) findCatalogTrack(ctx context.Context, track store.LibraryTrack) (*domain.CatalogTrack, string) {
	if track.ISRC != "" {
		if hit := s.searchFor(ctx, track.ISRC, func(c domain.CatalogTrack) bool {
			return strings.EqualFold(c.ISRC, track.ISRC)
		}); hit != nil {
			return hit, store.FixConfidenceExact
		}
	}

	query := strings.TrimSpace(track.Artist + " " + track.Title)
	if query == "" {
		return nil, ""
	}

	hit := s.searchFor(ctx, query, func(c domain.CatalogTrack) bool {
		if NormalizeMatchKey(c.Title) != track.TitleKey {
			return false
		}
		artistKey := NormalizeMatchKey(c.Artist)
		if artistKey == track.ArtistKey {
			return true
		}
		primary := PrimaryArtistKey(c.Artist)
		return primary != "" &&
			(primary == track.ArtistPrimaryKey || primary == track.ArtistKey)
	})
	if hit == nil {
		return nil, ""
	}
	return hit, store.FixConfidenceFuzzy
}

func (s *LibraryFixService) searchFor(ctx context.Context, query string, accept func(domain.CatalogTrack) bool) *domain.CatalogTrack {
	provider := s.catalog()
	if provider == nil {
		return nil
	}

	result, err := provider.Search(ctx, query, "track")
	if err != nil || result == nil {
		return nil
	}
	for i := range result.Tracks {
		if accept(result.Tracks[i]) {
			return &result.Tracks[i]
		}
	}
	return nil
}

// ProposeFixes compares one library track against its catalog counterpart and
// returns the changes worth making.
//
// A proposal is only produced when the catalog actually has a value: an empty
// source value is missing information, not a reason to blank a tag.
func ProposeFixes(track store.LibraryTrack, match domain.CatalogTrack, confidence string) []store.LibraryFix {
	sourceID := match.ID

	candidates := []struct {
		field   string
		current string
		propose string
	}{
		{FieldTitle, track.Title, match.Title},
		{FieldISRC, track.ISRC, match.ISRC},
		{FieldGenre, track.Genre, match.Genre},
		{FieldYear, intTag(track.Year), intTag(match.Year)},
		{FieldTrackNumber, intTag(track.TrackNumber), intTag(match.TrackNumber)},
		{FieldDiscNumber, intTag(track.DiscNumber), intTag(match.DiscNumber)},
	}

	fixes := make([]store.LibraryFix, 0, len(candidates))
	for _, c := range candidates {
		propose := strings.TrimSpace(c.propose)
		current := strings.TrimSpace(c.current)

		// A value differing only in case is not worth rewriting a file for.
		// The dry run found an ISRC proposed purely to change "Ussm11905848"
		// into "USSM11905848".
		if propose == "" || strings.EqualFold(propose, current) {
			continue
		}

		kind := store.FixKindChange
		if current == "" {
			kind = store.FixKindFill
		}

		fixes = append(fixes, store.LibraryFix{
			NavidromeID:   track.NavidromeID,
			Field:         c.field,
			CurrentValue:  current,
			ProposedValue: propose,
			Kind:          kind,
			Confidence:    confidence,
			SourceTrackID: sourceID,
			Status:        store.FixStatusProposed,
		})
	}

	return fixes
}

// AlwaysReview reports fields that must never be applied without a person
// looking, whatever the match confidence.
func AlwaysReview(field string) bool {
	return field == FieldTitle
}

// intTag renders a numeric tag, treating zero as absent rather than as the
// value nought — no track is track zero.
func intTag(v int) string {
	if v <= 0 {
		return ""
	}
	return strconv.Itoa(v)
}
