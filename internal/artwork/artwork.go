// Package artwork finds candidate album covers so one can be chosen by eye.
//
// There is no free Google Images API, and scraping results is both against
// Google's terms and unreliable to parse. Instead this queries the music
// catalogues that publish cover art deliberately — the same approach a desktop
// tagger takes — and presents everything found as one grid to pick from.
package artwork

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Candidate is one cover offered for choosing.
type Candidate struct {
	Source   string
	Title    string
	Artist   string
	ThumbURL string
	ImageURL string
	Size     int
}

// Searcher finds covers for one album.
type Searcher interface {
	Name() string
	Search(ctx context.Context, artist, album string) ([]Candidate, error)
}

// Finder queries every source at once and merges the results.
type Finder struct {
	sources []Searcher
	limit   int
}

func NewFinder(limit int, sources ...Searcher) *Finder {
	if limit <= 0 {
		limit = 12
	}
	return &Finder{sources: sources, limit: limit}
}

// Search asks every source in parallel.
//
// One source failing must not lose the others: a cover from anywhere is more
// useful than an error because a single service was unreachable.
func (f *Finder) Search(ctx context.Context, artist, album string) []Candidate {
	var (
		mu  sync.Mutex
		all []Candidate
		wg  sync.WaitGroup
	)

	for _, source := range f.sources {
		wg.Add(1)
		go func(s Searcher) {
			defer wg.Done()

			found, err := s.Search(ctx, artist, album)
			if err != nil {
				return
			}

			mu.Lock()
			all = append(all, found...)
			mu.Unlock()
		}(source)
	}
	wg.Wait()

	return f.rank(all)
}

// rank drops duplicates and puts the largest images first, since the point of
// changing a cover is usually to get a better one.
func (f *Finder) rank(in []Candidate) []Candidate {
	seen := make(map[string]bool, len(in))
	out := make([]Candidate, 0, len(in))

	for _, c := range in {
		if c.ImageURL == "" || seen[c.ImageURL] {
			continue
		}
		seen[c.ImageURL] = true
		if c.ThumbURL == "" {
			c.ThumbURL = c.ImageURL
		}
		out = append(out, c)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })

	if len(out) > f.limit {
		out = out[:f.limit]
	}
	return out
}

// httpClient is shared by the sources. Cover search is a foreground action, so
// it fails fast rather than leaving someone waiting.
var httpClient = &http.Client{Timeout: 15 * time.Second}

func getJSON(ctx context.Context, endpoint string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", endpoint, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func query(artist, album string) string {
	return strings.TrimSpace(artist + " " + album)
}

// ── iTunes ───────────────────────────────────────────────────────────────────

// ITunes searches Apple's public catalogue. No key, no quota worth worrying
// about, and its artwork URLs can be asked for at any size.
type ITunes struct{}

func (ITunes) Name() string { return "iTunes" }

func (i ITunes) Search(ctx context.Context, artist, album string) ([]Candidate, error) {
	endpoint := "https://itunes.apple.com/search?entity=album&limit=8&term=" +
		url.QueryEscape(query(artist, album))

	var resp struct {
		Results []struct {
			CollectionName string `json:"collectionName"`
			ArtistName     string `json:"artistName"`
			ArtworkURL100  string `json:"artworkUrl100"`
		} `json:"results"`
	}
	if err := getJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}

	out := make([]Candidate, 0, len(resp.Results))
	for _, r := range resp.Results {
		if r.ArtworkURL100 == "" {
			continue
		}
		// The 100x100 in the URL is just a size request; asking for 1000
		// returns the full-resolution cover.
		full := strings.Replace(r.ArtworkURL100, "100x100bb", "1000x1000bb", 1)
		out = append(out, Candidate{
			Source:   "iTunes",
			Title:    r.CollectionName,
			Artist:   r.ArtistName,
			ThumbURL: r.ArtworkURL100,
			ImageURL: full,
			Size:     1000,
		})
	}
	return out, nil
}

// ── Deezer ───────────────────────────────────────────────────────────────────

// Deezer publishes covers at several fixed sizes and needs no key.
type Deezer struct{}

func (Deezer) Name() string { return "Deezer" }

func (d Deezer) Search(ctx context.Context, artist, album string) ([]Candidate, error) {
	endpoint := "https://api.deezer.com/search/album?limit=8&q=" +
		url.QueryEscape(query(artist, album))

	var resp struct {
		Data []struct {
			Title  string `json:"title"`
			Cover  string `json:"cover_big"`
			CoverX string `json:"cover_xl"`
			Medium string `json:"cover_medium"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"data"`
	}
	if err := getJSON(ctx, endpoint, &resp); err != nil {
		return nil, err
	}

	out := make([]Candidate, 0, len(resp.Data))
	for _, r := range resp.Data {
		image := r.CoverX
		size := 1000
		if image == "" {
			image, size = r.Cover, 500
		}
		if image == "" {
			continue
		}
		out = append(out, Candidate{
			Source:   "Deezer",
			Title:    r.Title,
			Artist:   r.Artist.Name,
			ThumbURL: r.Medium,
			ImageURL: image,
			Size:     size,
		})
	}
	return out, nil
}
