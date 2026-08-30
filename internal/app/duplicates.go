package app

import (
	"fmt"
	"log/slog"
	"os"
	"sort"

	"github.com/cesargomez89/navidrums/internal/store"
)

// DuplicateService presents duplicate groups and deletes the copies chosen for
// removal.
//
// Deletion reuses the applier's gate rather than adding another: writing tags
// and removing files are the same permission in practice, and a second switch
// would be a second thing to forget to turn off.
type DuplicateService struct {
	db     *store.DB
	apply  *LibraryApplyService
	logger *slog.Logger
}

func NewDuplicateService(db *store.DB, apply *LibraryApplyService, logger *slog.Logger) *DuplicateService {
	return &DuplicateService{db: db, apply: apply, logger: logger}
}

// KeepScore rates how much a copy is worth keeping. Higher is better.
//
// It is advisory only: it orders the copies so the likely keeper is obvious,
// and never selects anything for deletion on its own. The signals are the ones
// that actually cost something to lose — a playlist reference, audio quality,
// and metadata that took effort to gather.
type KeepScore struct {
	Reasons []string
	Score   int
}

// ScoreCopy explains why one copy is worth more than another.
func ScoreCopy(c store.DuplicateCopy) KeepScore {
	out := KeepScore{}

	// A playlist reference outweighs everything else: deleting this file breaks
	// something the user built by hand, which no amount of extra bitrate
	// compensates for.
	if len(c.Playlists) > 0 {
		out.Score += 1000 * len(c.Playlists)
		out.Reasons = append(out.Reasons,
			fmt.Sprintf("in %d playlist(s)", len(c.Playlists)))
	}

	if c.Lossless {
		out.Score += 500
		out.Reasons = append(out.Reasons, "lossless")
	}

	// Bitrate separates copies of equal format. Scaled down so it never
	// outranks a playlist reference or losslessness.
	out.Score += c.BitRate / 10

	filled := 0
	for _, present := range []bool{
		c.ISRC != "", c.Genre != "", c.Year > 0,
		c.TrackNumber > 0, c.DiscNumber > 0,
	} {
		if present {
			filled++
		}
	}
	out.Score += filled * 20
	out.Reasons = append(out.Reasons, fmt.Sprintf("%d/5 tags", filled))

	if c.Size > 0 {
		out.Reasons = append(out.Reasons, humanSize(c.Size))
	}

	return out
}

// RankedCopy pairs a copy with its score and whether it is the suggested keeper.
type RankedCopy struct {
	Copy      store.DuplicateCopy
	Score     KeepScore
	Suggested bool
}

// RankedGroup is a duplicate group ordered best-first.
type RankedGroup struct {
	Title     string
	Artist    string
	Copies    []RankedCopy
	MaxDelta  int
	Uncertain bool
}

// Groups returns duplicate groups with the strongest copy first.
func (s *DuplicateService) Groups() ([]RankedGroup, error) {
	raw, err := s.db.DuplicateGroups()
	if err != nil {
		return nil, err
	}

	out := make([]RankedGroup, 0, len(raw))
	for _, group := range raw {
		ranked := RankedGroup{
			Title:    group.Title,
			Artist:   group.Artist,
			MaxDelta: group.MaxDelta,
			// Copies of one recording agree on duration closely. A wider
			// spread usually means different takes, so the group is flagged
			// rather than presented as settled.
			Uncertain: group.MaxDelta > 3,
		}

		for _, c := range group.Copies {
			ranked.Copies = append(ranked.Copies, RankedCopy{Copy: c, Score: ScoreCopy(c)})
		}

		sort.SliceStable(ranked.Copies, func(i, j int) bool {
			return ranked.Copies[i].Score.Score > ranked.Copies[j].Score.Score
		})

		// Only suggest a keeper where the group looks like genuine copies.
		if len(ranked.Copies) > 0 && !ranked.Uncertain {
			ranked.Copies[0].Suggested = true
		}

		out = append(out, ranked)
	}

	return out, nil
}

// Delete removes one file and forgets it.
//
// Deleting is the one operation here that cannot be undone — there is no
// backup of audio the way there is of tags — so it happens one file at a time,
// each chosen explicitly, and never in bulk.
func (s *DuplicateService) Delete(navidromeID string) error {
	if s.apply == nil {
		return ErrWriteDisabled
	}
	enabled, _, reason := s.apply.Status()
	if !enabled {
		return fmt.Errorf("%s", reason)
	}

	track, err := s.db.LookupLibraryTrack(navidromeID)
	if err != nil {
		return fmt.Errorf("could not find that track in the index: %w", err)
	}

	local, err := s.apply.ResolvePath(track.Path)
	if err != nil {
		return err
	}

	if err := os.Remove(local); err != nil {
		return fmt.Errorf("could not delete the file: %w", err)
	}

	if s.logger != nil {
		s.logger.Info("Deleted a duplicate",
			"title", track.Title, "album", track.Album, "path", local,
			"playlists", len(track.Playlists))
	}

	return s.db.ForgetLibraryTrack(navidromeID)
}

func humanSize(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
