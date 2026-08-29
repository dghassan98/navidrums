// Package subsonic is a read-only client for the Subsonic API that Navidrome
// speaks. It exists so Navidrums can tell what is already in the music library
// without touching it: every call here is a GET, so the protocol itself
// guarantees the library is not modified.
package subsonic

import (
	"context"
	"crypto/md5" //nolint:gosec // Subsonic specifies MD5 for its auth token
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNotConfigured reports that no Navidrome connection was configured. It is
// not a failure: the library index is optional, and everything else works
// without it.
var ErrNotConfigured = errors.New("navidrome is not configured")

// apiVersion is the Subsonic protocol level requested. 1.16.1 is what
// Navidrome implements and is old enough to be safe with other servers.
const apiVersion = "1.16.1"

// clientName identifies Navidrums in Navidrome's logs and active-client list.
const clientName = "navidrums"

// pageSize is the maximum Navidrome returns per request. The whole library
// comes back in a handful of calls at this size.
const pageSize = 500

type Client struct {
	http     *http.Client
	BaseURL  string
	user     string
	password string
}

func NewClient(baseURL, user, password string) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		user:     user,
		password: password,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Configured reports whether there is enough configuration to try a call.
func (c *Client) Configured() bool {
	return c != nil && c.BaseURL != "" && c.user != "" && c.password != ""
}

// Song is the subset of a Subsonic song used for library matching.
type Song struct {
	ID     string
	Title  string
	Artist string
	Album  string
	ISRC   string
	Suffix string
	Path   string
	// Genre, TrackNumber and DiscNumber exist so the cleanup can tell an
	// absent tag from a populated one. They are not used for matching.
	Genre       string
	Year        int
	Duration    int
	BitRate     int
	BitDepth    int
	TrackNumber int
	DiscNumber  int
	Lossless    bool
}

// Ping checks the connection and credentials.
func (c *Client) Ping(ctx context.Context) error {
	var resp apiEnvelope
	return c.get(ctx, "ping", nil, &resp)
}

// ServerInfo describes the server that answered, for the settings panel.
type ServerInfo struct {
	Type          string
	Version       string
	OpenSubsonic  bool
	SongCount     int
	AlbumCount    int
	ArtistCount   int
	CheckedAt     time.Time
	ReachedServer bool
}

// Probe reports what the connection can see, so a misconfiguration is
// distinguishable from an empty library.
func (c *Client) Probe(ctx context.Context) (*ServerInfo, error) {
	var ping apiEnvelope
	if err := c.get(ctx, "ping", nil, &ping); err != nil {
		return nil, err
	}

	info := &ServerInfo{
		Type:          ping.Response.Type,
		Version:       ping.Response.ServerVersion,
		OpenSubsonic:  ping.Response.OpenSubsonic,
		CheckedAt:     time.Now(),
		ReachedServer: true,
	}

	var scan apiEnvelope
	if err := c.get(ctx, "getScanStatus", nil, &scan); err == nil {
		info.SongCount = scan.Response.ScanStatus.Count
	}

	return info, nil
}

// Songs streams the whole library, one page at a time. The callback runs per
// page so the caller can write as it goes instead of holding the library in
// memory.
//
// An empty query is Navidrome's match-everything, which makes search3 a cheap
// full enumeration rather than a search.
func (c *Client) Songs(ctx context.Context, page func([]Song) error) error {
	for offset := 0; ; offset += pageSize {
		params := url.Values{}
		params.Set("query", "")
		params.Set("songCount", strconv.Itoa(pageSize))
		params.Set("songOffset", strconv.Itoa(offset))
		params.Set("artistCount", "0")
		params.Set("albumCount", "0")

		var resp apiEnvelope
		if err := c.get(ctx, "search3", params, &resp); err != nil {
			return err
		}

		raw := resp.Response.SearchResult3.Song
		if len(raw) == 0 {
			return nil
		}

		songs := make([]Song, 0, len(raw))
		for i := range raw {
			songs = append(songs, raw[i].toSong())
		}
		if err := page(songs); err != nil {
			return err
		}

		if len(raw) < pageSize {
			return nil
		}
	}
}

func (c *Client) get(ctx context.Context, endpoint string, params url.Values, target *apiEnvelope) error {
	if !c.Configured() {
		return ErrNotConfigured
	}

	if params == nil {
		params = url.Values{}
	}
	salt, err := randomSalt()
	if err != nil {
		return err
	}
	params.Set("u", c.user)
	params.Set("t", authToken(c.password, salt))
	params.Set("s", salt)
	params.Set("v", apiVersion)
	params.Set("c", clientName)
	params.Set("f", "json")

	endpointURL := fmt.Sprintf("%s/rest/%s?%s", c.BaseURL, endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("navidrome request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("navidrome returned %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("navidrome sent an unreadable response: %w", err)
	}

	// Subsonic reports failures in the body with HTTP 200, so the status code
	// alone never proves success.
	if target.Response.Status != "ok" {
		if target.Response.Error != nil {
			return fmt.Errorf("navidrome error %d: %s",
				target.Response.Error.Code, target.Response.Error.Message)
		}
		return fmt.Errorf("navidrome reported status %q", target.Response.Status)
	}

	return nil
}

// authToken is the Subsonic scheme: md5 of the password with a per-request
// salt, so the password itself never crosses the wire.
func authToken(password, salt string) string {
	sum := md5.Sum([]byte(password + salt)) //nolint:gosec // required by the Subsonic API
	return hex.EncodeToString(sum[:])
}

func randomSalt() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("could not generate an auth salt: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
