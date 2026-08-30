package store

import (
	"github.com/jmoiron/sqlx"
)

// PlaylistEntry is one track membership of one playlist.
type PlaylistEntry struct {
	NavidromeID  string
	PlaylistID   string
	PlaylistName string
}

// ReplacePlaylistMembership swaps the record of which tracks sit in which
// playlists.
func (db *DB) ReplacePlaylistMembership(entries []PlaylistEntry) error {
	tx, err := db.root.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM library_playlist_tracks`); err != nil {
		return err
	}

	for i := range entries {
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO library_playlist_tracks
				(navidrome_id, playlist_id, playlist_name)
			VALUES (?, ?, ?)`,
			entries[i].NavidromeID, entries[i].PlaylistID, entries[i].PlaylistName); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DuplicateCopy is one file within a group of duplicates, carrying everything
// needed to judge whether it is the one worth keeping.
type DuplicateCopy struct {
	NavidromeID string `db:"navidrome_id"`
	Title       string `db:"title"`
	Album       string `db:"album"`
	Artist      string `db:"artist"`
	Path        string `db:"path"`
	Suffix      string `db:"suffix"`
	AddedAt     string `db:"added_at"`
	Genre       string `db:"genre"`
	ISRC        string `db:"isrc"`
	Playlists   []string
	Size        int64 `db:"size"`
	Duration    int   `db:"duration"`
	BitRate     int   `db:"bit_rate"`
	BitDepth    int   `db:"bit_depth"`
	Year        int   `db:"year"`
	TrackNumber int   `db:"track_number"`
	DiscNumber  int   `db:"disc_number"`
	Lossless    bool  `db:"lossless"`
}

// DuplicateGroup is a set of files that appear to be the same recording.
type DuplicateGroup struct {
	Key      string
	Artist   string
	Title    string
	Copies   []DuplicateCopy
	MaxDelta int
}

// DuplicateGroups finds files that look like copies of the same recording.
//
// Grouping is on the strict key, which keeps "(Instrumental)" and "(Live at
// Dublin)" distinct. The catalogue key collapses those deliberately so a
// library track can be matched against a release, and grouping by it would
// offer three different recordings as copies of one another — a mistake that
// costs a file rather than a tag.
//
// Duration is reported rather than used to filter. Copies of one recording
// agree on it closely; a group that disagrees usually holds a different take,
// and that is a judgement for a person rather than a rule.
func (db *DB) DuplicateGroups() ([]DuplicateGroup, error) {
	var rows []struct {
		DuplicateCopy
		StrictKey string `db:"strict_key"`
		ArtistKey string `db:"artist_key"`
	}

	if err := db.Select(&rows, `
		SELECT lt.navidrome_id, lt.strict_key, lt.artist_key,
		       COALESCE(lt.title,'') AS title, COALESCE(lt.album,'') AS album,
		       COALESCE(lt.artist,'') AS artist, COALESCE(lt.path,'') AS path,
		       COALESCE(lt.suffix,'') AS suffix, COALESCE(lt.added_at,'') AS added_at,
		       COALESCE(lt.genre,'') AS genre, COALESCE(lt.isrc,'') AS isrc,
		       COALESCE(lt.size,0) AS size, COALESCE(lt.duration,0) AS duration,
		       COALESCE(lt.bit_rate,0) AS bit_rate, COALESCE(lt.bit_depth,0) AS bit_depth,
		       COALESCE(lt.year,0) AS year, COALESCE(lt.track_number,0) AS track_number,
		       COALESCE(lt.disc_number,0) AS disc_number, lt.lossless
		FROM library_tracks lt
		JOIN (
			SELECT strict_key, artist_key
			FROM library_tracks
			WHERE strict_key IS NOT NULL AND strict_key <> ''
			GROUP BY strict_key, artist_key
			HAVING COUNT(*) > 1
		) d ON d.strict_key = lt.strict_key AND d.artist_key = lt.artist_key
		ORDER BY lt.strict_key, lt.artist_key, lt.navidrome_id`); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(rows))
	for i := range rows {
		ids = append(ids, rows[i].NavidromeID)
	}
	memberships, err := db.playlistsFor(ids)
	if err != nil {
		return nil, err
	}

	var groups []DuplicateGroup
	currentKey := ""

	for i := range rows {
		key := rows[i].StrictKey + "|" + rows[i].ArtistKey
		if key != currentKey {
			groups = append(groups, DuplicateGroup{
				Key:    key,
				Artist: rows[i].Artist,
				Title:  rows[i].Title,
			})
			currentKey = key
		}

		entry := rows[i].DuplicateCopy
		entry.Playlists = memberships[entry.NavidromeID]
		groups[len(groups)-1].Copies = append(groups[len(groups)-1].Copies, entry)
	}

	for i := range groups {
		groups[i].MaxDelta = durationSpread(groups[i].Copies)
	}

	return groups, nil
}

// durationSpread is the largest gap in seconds between copies, which is the
// clearest signal that a group holds different recordings rather than copies.
func durationSpread(copies []DuplicateCopy) int {
	if len(copies) == 0 {
		return 0
	}
	low, high := copies[0].Duration, copies[0].Duration
	for _, c := range copies[1:] {
		if c.Duration < low {
			low = c.Duration
		}
		if c.Duration > high {
			high = c.Duration
		}
	}
	return high - low
}

func (db *DB) playlistsFor(ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}

	query, args, err := sqlx.In(
		`SELECT navidrome_id, playlist_name FROM library_playlist_tracks
		 WHERE navidrome_id IN (?) ORDER BY playlist_name`, ids)
	if err != nil {
		return nil, err
	}

	rows, err := db.Queryx(db.Rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = append(out[id], name)
	}
	return out, rows.Err()
}

// ForgetLibraryTrack removes a track from the index after its file is deleted,
// so it does not linger as a duplicate until the next sync.
func (db *DB) ForgetLibraryTrack(navidromeID string) error {
	tx, err := db.root.Beginx()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, q := range []string{
		`DELETE FROM library_tracks WHERE navidrome_id = ?`,
		`DELETE FROM library_playlist_tracks WHERE navidrome_id = ?`,
		`DELETE FROM library_scan_state WHERE navidrome_id = ?`,
		`DELETE FROM library_fixes WHERE navidrome_id = ?`,
	} {
		if _, err := tx.Exec(q, navidromeID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// LookupLibraryTrack returns one indexed track by id.
func (db *DB) LookupLibraryTrack(navidromeID string) (*DuplicateCopy, error) {
	var out DuplicateCopy
	err := db.Get(&out, `
		SELECT navidrome_id, COALESCE(title,'') AS title, COALESCE(album,'') AS album,
		       COALESCE(artist,'') AS artist, COALESCE(path,'') AS path,
		       COALESCE(suffix,'') AS suffix, COALESCE(added_at,'') AS added_at,
		       COALESCE(genre,'') AS genre, COALESCE(isrc,'') AS isrc,
		       COALESCE(size,0) AS size, COALESCE(duration,0) AS duration,
		       COALESCE(bit_rate,0) AS bit_rate, COALESCE(bit_depth,0) AS bit_depth,
		       COALESCE(year,0) AS year, COALESCE(track_number,0) AS track_number,
		       COALESCE(disc_number,0) AS disc_number, lossless
		FROM library_tracks WHERE navidrome_id = ?`, navidromeID)
	if err != nil {
		return nil, err
	}

	if names, err := db.playlistsFor([]string{navidromeID}); err == nil {
		out.Playlists = names[navidromeID]
	}
	return &out, nil
}
