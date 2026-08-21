package app

import (
	"github.com/cesargomez89/navidrums/internal/domain"
)

// JobView is a job decorated with what it is actually downloading. A job only
// stores a provider ID, which is meaningless to read in the queue, so the
// matching track is looked up and its names carried alongside.
type JobView struct {
	*domain.Job

	Title  string
	Artist string
	Album  string
}

// HasNames reports whether a name was resolved, so templates can fall back to
// the raw source ID for jobs whose track is not stored yet.
func (v *JobView) HasNames() bool {
	return v.Title != "" || v.Album != "" || v.Artist != ""
}

// PrimaryLabel is what to show as the job's headline.
func (v *JobView) PrimaryLabel() string {
	switch {
	case v.Title != "":
		return v.Title
	case v.Album != "":
		return v.Album
	default:
		return v.GetSourceID()
	}
}

// SecondaryLabel is the supporting line: artist, album, or both.
func (v *JobView) SecondaryLabel() string {
	switch {
	case v.Artist != "" && v.Album != "" && v.Title != "":
		return v.Artist + " — " + v.Album
	case v.Artist != "":
		return v.Artist
	case v.Album != "" && v.Title != "":
		return v.Album
	default:
		return ""
	}
}

// decorateJobs resolves names for a page of jobs in as few queries as possible.
func (s *JobService) decorateJobs(jobs []*domain.Job) []*JobView {
	views := make([]*JobView, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, &JobView{Job: job})
	}

	sourceIDs := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if id := job.GetSourceID(); id != "" {
			sourceIDs = append(sourceIDs, id)
		}
	}

	tracks, err := s.Repo.GetTracksByProviderIDs(sourceIDs)
	if err != nil {
		// Names are decoration: a lookup failure should not empty the queue.
		return views
	}

	for _, view := range views {
		sourceID := view.GetSourceID()
		if sourceID == "" {
			continue
		}

		if track, ok := tracks[sourceID]; ok {
			view.Title = track.Title
			view.Artist = firstNonEmpty(track.Artist, track.AlbumArtist)
			view.Album = track.Album
			continue
		}

		// Container jobs point at an album rather than a track, so borrow the
		// names from any track already stored for that album.
		if view.Type != domain.JobTypeTrack {
			if track, albumErr := s.Repo.GetAlbumSampleTrack(sourceID); albumErr == nil && track != nil {
				view.Album = track.Album
				view.Artist = firstNonEmpty(track.AlbumArtist, track.Artist)
			}
		}
	}

	return views
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ListActiveJobViews lists queued and running jobs with their names resolved.
func (s *JobService) ListActiveJobViews(page, pageSize int) ([]*JobView, int, error) {
	jobs, total, err := s.ListActiveJobs(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	return s.decorateJobs(jobs), total, nil
}

// ListFinishedJobViews lists finished jobs with their names resolved.
func (s *JobService) ListFinishedJobViews(page, pageSize int) ([]*JobView, int, error) {
	jobs, total, err := s.ListFinishedJobs(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	return s.decorateJobs(jobs), total, nil
}
