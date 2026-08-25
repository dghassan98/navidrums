package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/store"
	"github.com/cesargomez89/navidrums/internal/subsonic"
)

// LibraryService keeps a read-only index of the music library, so browse pages
// can tell what is already owned and at what quality.
//
// Navidrums never modifies the library. This service only reads from Navidrome
// and writes into Navidrums' own database.
type LibraryService struct {
	client *subsonic.Client
	db     *store.DB
	logger *slog.Logger

	mu       sync.Mutex
	syncing  bool
	lastErr  error
	lastRun  time.Time
	lastRows int
}

func NewLibraryService(client *subsonic.Client, db *store.DB, logger *slog.Logger) *LibraryService {
	return &LibraryService{client: client, db: db, logger: logger}
}

// Configured reports whether a Navidrome connection was set up at all.
func (s *LibraryService) Configured() bool {
	return s != nil && s.client.Configured()
}

// SyncStatus is what the settings panel shows.
type SyncStatus struct {
	LastRun    time.Time
	LastError  string
	Stats      *store.LibraryStats
	Configured bool
	Syncing    bool
}

func (s *LibraryService) Status() SyncStatus {
	s.mu.Lock()
	status := SyncStatus{
		Configured: s.Configured(),
		Syncing:    s.syncing,
		LastRun:    s.lastRun,
	}
	if s.lastErr != nil {
		status.LastError = s.lastErr.Error()
	}
	s.mu.Unlock()

	if s.db != nil {
		if stats, err := s.db.LibraryStats(); err == nil {
			status.Stats = stats
		}
	}
	return status
}

// Sync rebuilds the index from Navidrome. It is safe to call concurrently:
// a second caller returns immediately rather than duplicating the work.
func (s *LibraryService) Sync(ctx context.Context) error {
	if !s.Configured() {
		return subsonic.ErrNotConfigured
	}

	s.mu.Lock()
	if s.syncing {
		s.mu.Unlock()
		return errors.New("a library sync is already running")
	}
	s.syncing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.syncing = false
		s.lastRun = time.Now()
		s.mu.Unlock()
	}()

	started := time.Now()
	tracks := make([]store.LibraryTrack, 0, 4096)

	err := s.client.Songs(ctx, func(page []subsonic.Song) error {
		for i := range page {
			tracks = append(tracks, toLibraryTrack(page[i]))
		}
		return nil
	})
	if err != nil {
		s.setErr(fmt.Errorf("library sync failed: %w", err))
		return err
	}

	// Only replace the index once every page has arrived. A partial index
	// under-reports what is owned, which is the failure that costs a
	// re-download.
	if err := s.db.ReplaceLibraryTracks(tracks); err != nil {
		s.setErr(fmt.Errorf("library index write failed: %w", err))
		return err
	}

	s.mu.Lock()
	s.lastErr = nil
	s.lastRows = len(tracks)
	s.mu.Unlock()

	if s.logger != nil {
		s.logger.Info("Library index synced",
			"tracks", len(tracks), "took", time.Since(started).Round(time.Millisecond))
	}
	return nil
}

func (s *LibraryService) setErr(err error) {
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
	if s.logger != nil {
		s.logger.Error("Library sync failed", "error", err)
	}
}

func toLibraryTrack(song subsonic.Song) store.LibraryTrack {
	return store.LibraryTrack{
		NavidromeID: song.ID,
		ISRC:        song.ISRC,
		TitleKey:    NormalizeMatchKey(song.Title),
		ArtistKey:   NormalizeMatchKey(song.Artist),
		AlbumKey:    NormalizeMatchKey(song.Album),
		Title:       song.Title,
		Artist:      song.Artist,
		Album:       song.Album,
		Year:        song.Year,
		Duration:    song.Duration,
		Suffix:      song.Suffix,
		BitRate:     song.BitRate,
		BitDepth:    song.BitDepth,
		Lossless:    song.Lossless,
		Path:        song.Path,
	}
}

// AlbumOwnership summarises how much of an album is already held.
type AlbumOwnership struct {
	Owned    int
	Total    int
	Lossless int
}

// Complete reports whether every track is present.
func (a AlbumOwnership) Complete() bool { return a.Total > 0 && a.Owned >= a.Total }

// UpgradeWorthwhile reports that the album is held but not losslessly, so
// downloading it is still an improvement rather than a duplicate.
func (a AlbumOwnership) UpgradeWorthwhile() bool {
	return a.Owned > 0 && a.Lossless < a.Owned
}

// LibraryIndex answers ownership questions for a page of catalog tracks.
type LibraryIndex interface {
	OwnershipFor(tracks []domain.CatalogTrack) (AlbumOwnership, error)
}

// OwnershipFor matches a catalog album's tracks against the library.
//
// ISRC is trusted outright. Everything else falls back to normalised title and
// artist, which is why NormalizeMatchKey errs toward keeping distinct tracks
// apart: a false match here means skipping an album you do not have.
func (s *LibraryService) OwnershipFor(tracks []domain.CatalogTrack) (AlbumOwnership, error) {
	out := AlbumOwnership{Total: len(tracks)}
	if s == nil || s.db == nil || len(tracks) == 0 {
		return out, nil
	}

	isrcs := make([]string, 0, len(tracks))
	titleKeys := make([]string, 0, len(tracks))
	artistKeys := make([]string, 0, len(tracks))
	for i := range tracks {
		if tracks[i].ISRC != "" {
			isrcs = append(isrcs, tracks[i].ISRC)
		}
		titleKeys = append(titleKeys, NormalizeMatchKey(tracks[i].Title))
		artistKeys = append(artistKeys, NormalizeMatchKey(tracks[i].Artist))
	}

	rows, err := s.db.FindLibraryMatches(isrcs, titleKeys, artistKeys)
	if err != nil {
		return out, err
	}

	byISRC := make(map[string]bool, len(rows))
	byName := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.ISRC != "" {
			byISRC[row.ISRC] = byISRC[row.ISRC] || row.Lossless
		}
		key := row.TitleKey + "\x00" + row.ArtistKey
		byName[key] = byName[key] || row.Lossless
	}

	for i := range tracks {
		lossless, found := false, false

		if isrc := tracks[i].ISRC; isrc != "" {
			if l, ok := byISRC[isrc]; ok {
				found, lossless = true, l
			}
		}
		if !found {
			key := NormalizeMatchKey(tracks[i].Title) + "\x00" + NormalizeMatchKey(tracks[i].Artist)
			if l, ok := byName[key]; ok {
				found, lossless = true, l
			}
		}

		if found {
			out.Owned++
			if lossless {
				out.Lossless++
			}
		}
	}

	return out, nil
}
