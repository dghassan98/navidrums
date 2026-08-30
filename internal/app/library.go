package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

	// Playlist membership is fetched alongside the tracks because it is what
	// makes a deletion decision safe: removing a file a playlist points at
	// breaks the playlist, and nothing about the file itself says so.
	if err := s.syncPlaylists(ctx); err != nil {
		s.setErr(fmt.Errorf("library synced, but playlists did not: %w", err))
	}

	if s.logger != nil {
		s.logger.Info("Library index synced",
			"tracks", len(tracks), "took", time.Since(started).Round(time.Millisecond))
	}
	return nil
}

func (s *LibraryService) syncPlaylists(ctx context.Context) error {
	playlists, err := s.client.Playlists(ctx)
	if err != nil {
		return err
	}

	entries := make([]store.PlaylistEntry, 0, 1024)
	for _, playlist := range playlists {
		for _, songID := range playlist.SongIDs {
			entries = append(entries, store.PlaylistEntry{
				NavidromeID:  songID,
				PlaylistID:   playlist.ID,
				PlaylistName: playlist.Name,
			})
		}
	}

	if s.logger != nil {
		s.logger.Info("Playlist membership synced",
			"playlists", len(playlists), "entries", len(entries))
	}
	return s.db.ReplacePlaylistMembership(entries)
}

func (s *LibraryService) setErr(err error) {
	s.mu.Lock()
	s.lastErr = err
	s.mu.Unlock()
	if s.logger != nil {
		s.logger.Error("Library sync failed", "error", err)
	}
}

// TriggerRescan asks the music server to re-read the library, so changes just
// written to files become visible instead of appearing to have done nothing.
func (s *LibraryService) TriggerRescan(ctx context.Context) error {
	if !s.Configured() {
		return subsonic.ErrNotConfigured
	}

	err := s.client.StartScan(ctx)
	if err != nil && strings.Contains(err.Error(), "not authorized") {
		// Navidrome only lets administrators start a scan. Nothing else here
		// needs that, so the account is otherwise correctly unprivileged.
		return fmt.Errorf("%w — starting a scan requires an admin account in "+
			"Navidrome; either grant this user admin or rescan manually", err)
	}
	return err
}

func toLibraryTrack(song subsonic.Song) store.LibraryTrack {
	return store.LibraryTrack{
		NavidromeID:      song.ID,
		ISRC:             song.ISRC,
		TitleKey:         NormalizeMatchKey(song.Title),
		ArtistKey:        NormalizeMatchKey(song.Artist),
		ArtistPrimaryKey: PrimaryArtistKey(song.Artist),
		AlbumKey:         NormalizeMatchKey(song.Album),
		Title:            song.Title,
		Artist:           song.Artist,
		Album:            song.Album,
		Genre:            song.Genre,
		StrictKey:        StrictMatchKey(song.Title),
		AddedAt:          song.Created,
		Size:             song.Size,
		Year:             song.Year,
		TrackNumber:      song.TrackNumber,
		DiscNumber:       song.DiscNumber,
		Duration:         song.Duration,
		Suffix:           song.Suffix,
		BitRate:          song.BitRate,
		BitDepth:         song.BitDepth,
		Lossless:         song.Lossless,
		Path:             song.Path,
	}
}

// AlbumOwnership summarises how much of an album is already held.
type AlbumOwnership struct {
	Owned    int
	Total    int
	Lossless int
}

// Missing is how many tracks are not held at all.
func (a AlbumOwnership) Missing() int {
	if a.Total <= a.Owned {
		return 0
	}
	return a.Total - a.Owned
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

// OwnershipFor matches a catalog album's tracks against the library, and marks
// each track in place so callers can render per-track state.
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
		if primary := PrimaryArtistKey(tracks[i].Artist); primary != "" {
			artistKeys = append(artistKeys, primary)
		}
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
		// Index under both the full credit and the lead artist, so either
		// side's credit style can find the other. A library file tagged
		// "Dua Lipa, Angèle" has to be findable from a Qobuz "Dua Lipa".
		for _, artistKey := range []string{row.ArtistKey, row.ArtistPrimaryKey} {
			if artistKey == "" {
				continue
			}
			key := row.TitleKey + "\x00" + artistKey
			byName[key] = byName[key] || row.Lossless
		}
	}

	for i := range tracks {
		lossless, found := false, false

		if isrc := tracks[i].ISRC; isrc != "" {
			if l, ok := byISRC[isrc]; ok {
				found, lossless = true, l
			}
		}
		if !found {
			titleKey := NormalizeMatchKey(tracks[i].Title)
			for _, artistKey := range []string{
				NormalizeMatchKey(tracks[i].Artist),
				PrimaryArtistKey(tracks[i].Artist),
			} {
				if artistKey == "" {
					continue
				}
				if l, ok := byName[titleKey+"\x00"+artistKey]; ok {
					found, lossless = true, l
					break
				}
			}
		}

		// Mark the track itself so the album page can show which ones are
		// held, not just how many.
		tracks[i].Owned = found
		tracks[i].OwnedLossless = found && lossless

		if found {
			out.Owned++
			if lossless {
				out.Lossless++
			}
		}
	}

	return out, nil
}
