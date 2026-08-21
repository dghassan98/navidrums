package catalog

import (
	"context"
	"crypto/md5" //nolint:gosec // Qobuz specifies MD5 for its password hash and request signature
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cesargomez89/navidrums/internal/constants"
	"github.com/cesargomez89/navidrums/internal/domain"
	"github.com/cesargomez89/navidrums/internal/httpclient"
)

var (
	// ErrQobuzCredentialsMissing reports that the qobuz-direct provider was
	// selected without enough configuration to identify the account.
	ErrQobuzCredentialsMissing = errors.New("qobuz-direct needs QOBUZ_APP_ID plus either QOBUZ_AUTH_TOKEN, or QOBUZ_EMAIL and QOBUZ_PASSWORD")

	// ErrQobuzSecretMissing reports that downloads cannot be signed. Metadata
	// still works without the app secret.
	ErrQobuzSecretMissing = errors.New("qobuz-direct needs QOBUZ_APP_SECRET to sign download requests")

	// ErrQobuzTokenRejected reports that QOBUZ_AUTH_TOKEN was refused and
	// cannot be refreshed, since no password was configured.
	ErrQobuzTokenRejected = errors.New("qobuz rejected QOBUZ_AUTH_TOKEN; grab a fresh one from the web player")

	// ErrQobuzIneligible reports an account that cannot stream files. Qobuz
	// only hands out file URLs to paying subscribers.
	ErrQobuzIneligible = errors.New("qobuz account is not eligible to stream tracks (a paid subscription is required)")

	// ErrQobuzNotStreamable reports that Qobuz refused this particular track.
	ErrQobuzNotStreamable = errors.New("qobuz refused to stream this track")
)

// qobuzPlaylistTrackLimit caps how many playlist tracks are fetched in one call.
const qobuzPlaylistTrackLimit = 500

// similarArtistsLimit matches what the other providers request.
const similarArtistsLimit = 8

// QobuzCredentials holds everything the Qobuz API needs to identify the client
// and the account. AppID and AppSecret identify the *application*; Qobuz does
// not issue them per user, so they are supplied through configuration.
//
// The account can be identified either by an AuthToken lifted from a logged-in
// web player, or by Email plus PasswordMD5 which are exchanged for one.
type QobuzCredentials struct {
	AppID       string
	AppSecret   string
	Email       string
	PasswordMD5 string
	AuthToken   string
}

// CanAuthenticate reports whether the account can be identified at all.
func (c QobuzCredentials) CanAuthenticate() bool {
	if c.AppID == "" {
		return false
	}
	return c.AuthToken != "" || (c.Email != "" && c.PasswordMD5 != "")
}

// CanSign reports whether file-URL requests can be signed. Without the app
// secret, metadata still works but downloads cannot.
func (c QobuzCredentials) CanSign() bool {
	return c.AppSecret != ""
}

// UsesAuthToken reports whether a token was supplied directly, meaning there is
// no password to log in with if the token is rejected.
func (c QobuzCredentials) UsesAuthToken() bool {
	return c.AuthToken != ""
}

// QobuzDirectProvider talks to the official Qobuz API with the user's own
// subscription, rather than through a shared third-party proxy. It signs
// file-URL requests with the app secret and caches the auth token from login.
type QobuzDirectProvider struct {
	client  *httpclient.Client
	BaseURL string
	creds   QobuzCredentials

	mu    sync.Mutex
	token string
}

// NewQobuzDirectProvider builds a provider for the official Qobuz API, falling
// back to the standard endpoint when no URL is configured.
func NewQobuzDirectProvider(baseURL string, creds QobuzCredentials) *QobuzDirectProvider {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		trimmed = constants.QobuzDirectDefaultURL
	}

	return &QobuzDirectProvider{
		BaseURL: trimmed,
		creds:   creds,
		client: httpclient.NewClient(&http.Client{
			Timeout: 20 * time.Second,
		}, 500*time.Millisecond),
	}
}

// QobuzPasswordHash returns the MD5 hex digest Qobuz expects in place of a
// plaintext password.
func QobuzPasswordHash(password string) string {
	sum := md5.Sum([]byte(password)) //nolint:gosec // required by the Qobuz API
	return hex.EncodeToString(sum[:])
}

func (p *QobuzDirectProvider) Search(ctx context.Context, query string, searchType string) (*domain.SearchResult, error) {
	endpoint := "album/search"
	switch searchType {
	case "artist":
		endpoint = "artist/search"
	case "track":
		endpoint = "track/search"
	case "playlist":
		endpoint = "playlist/search"
	case "album", "", "all":
		endpoint = "album/search"
	}

	params := url.Values{}
	params.Set("query", query)
	params.Set("limit", strconv.Itoa(constants.MaxSearchResults))

	var resp QobuzSearchData
	if err := p.authedGet(ctx, endpoint, params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz search failed: %w", err)
	}

	return resp.ToDomain(), nil
}

func (p *QobuzDirectProvider) GetAlbum(ctx context.Context, id string) (*domain.Album, error) {
	params := url.Values{}
	params.Set("album_id", id)

	var resp QobuzAlbumResponse
	if err := p.authedGet(ctx, "album/get", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get album failed: %w", err)
	}
	if resp.ID == "" {
		return nil, fmt.Errorf("qobuz album not found: %s", id)
	}

	return resp.ToDomain(), nil
}

func (p *QobuzDirectProvider) GetTrack(ctx context.Context, id string) (*domain.CatalogTrack, error) {
	params := url.Values{}
	params.Set("track_id", id)

	var resp QobuzTrackResponse
	if err := p.authedGet(ctx, "track/get", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get track failed: %w", err)
	}
	if resp.ID == 0 {
		return nil, fmt.Errorf("qobuz track not found: %s", id)
	}

	track := resp.ToDomain()
	return &track, nil
}

func (p *QobuzDirectProvider) GetArtist(ctx context.Context, id string) (*domain.Artist, error) {
	params := url.Values{}
	params.Set("artist_id", id)
	params.Set("extra", "albums")
	params.Set("limit", strconv.Itoa(constants.MaxSearchResults))
	params.Set("offset", "0")

	var resp QobuzDirectArtistResponse
	if err := p.authedGet(ctx, "artist/get", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get artist failed: %w", err)
	}
	if resp.ID == 0 {
		return nil, fmt.Errorf("qobuz artist not found: %s", id)
	}

	artist := resp.ToDomain()

	// artist/get has no top tracks and sometimes no portrait; artist/page has
	// both. Treat it as optional so a failure there still yields the artist.
	if page, pageErr := p.artistPage(ctx, id); pageErr == nil {
		artist.TopTracks = page.ToTopTracks()
		if artist.PictureURL == "" {
			artist.PictureURL = page.PortraitURL()
		}
	}

	return artist, nil
}

// artistPage fetches the richer artist view used by the Qobuz web player.
func (p *QobuzDirectProvider) artistPage(ctx context.Context, id string) (*QobuzArtistPageResponse, error) {
	params := url.Values{}
	params.Set("artist_id", id)
	params.Set("sort", "release_date")

	var resp QobuzArtistPageResponse
	if err := p.authedGet(ctx, "artist/page", params, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (p *QobuzDirectProvider) GetPlaylist(ctx context.Context, id string) (*domain.Playlist, error) {
	params := url.Values{}
	params.Set("playlist_id", id)
	params.Set("extra", "tracks")
	params.Set("limit", strconv.Itoa(qobuzPlaylistTrackLimit))
	params.Set("offset", "0")

	var resp QobuzDirectPlaylistResponse
	if err := p.authedGet(ctx, "playlist/get", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get playlist failed: %w", err)
	}
	if resp.ID == 0 {
		return nil, fmt.Errorf("qobuz playlist not found: %s", id)
	}

	return resp.ToDomain(), nil
}

func (p *QobuzDirectProvider) GetStream(ctx context.Context, trackID string, isrc string, quality string) (io.ReadCloser, string, error) {
	// Report the more fundamental gap first: an account that cannot be
	// identified at all, before complaining about the signing secret.
	if !p.creds.CanAuthenticate() {
		return nil, "", ErrQobuzCredentialsMissing
	}

	tid := p.resolveTrackID(ctx, trackID, isrc)

	fileURL, mimeType, err := p.trackFileURL(ctx, tid, quality)
	if err != nil {
		return nil, "", err
	}

	resp, err := p.openStreamURL(ctx, fileURL)
	if err != nil {
		return nil, "", err
	}

	if mimeType == "" {
		mimeType = resp.Header.Get("Content-Type")
	}
	if mimeType == "" {
		mimeType = constants.MimeTypeFLAC
	}

	return withSize(resp.Body, resp.ContentLength), mimeType, nil
}

// trackFileURL asks Qobuz for a time-limited CDN URL. The request must carry a
// timestamp and an MD5 signature computed over the same values with the app
// secret appended.
func (p *QobuzDirectProvider) trackFileURL(ctx context.Context, trackID string, quality string) (string, string, error) {
	if !p.creds.CanSign() {
		return "", "", ErrQobuzSecretMissing
	}

	formatID := qobuzDirectFormatID(quality)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	params := url.Values{}
	params.Set("request_ts", timestamp)
	params.Set("request_sig", qobuzRequestSignature(formatID, trackID, timestamp, p.creds.AppSecret))
	params.Set("track_id", trackID)
	params.Set("format_id", strconv.Itoa(formatID))
	params.Set("intent", "stream")

	var resp QobuzFileURLResponse
	if err := p.authedGet(ctx, "track/getFileUrl", params, &resp); err != nil {
		return "", "", fmt.Errorf("qobuz file url request failed: %w", err)
	}

	if resp.URL == "" {
		if len(resp.Restrictions) > 0 {
			return "", "", fmt.Errorf("%w: track %s (%s)", ErrQobuzNotStreamable, trackID, resp.Restrictions[0].Code)
		}
		return "", "", fmt.Errorf("%w: track %s", ErrQobuzNotStreamable, trackID)
	}

	return resp.URL, resp.MimeType, nil
}

// qobuzRequestSignature builds the MD5 signature Qobuz requires on
// track/getFileUrl: the endpoint, then each parameter name and value in
// alphabetical order, then the timestamp and the app secret.
func qobuzRequestSignature(formatID int, trackID string, timestamp string, appSecret string) string {
	raw := fmt.Sprintf("trackgetFileUrlformat_id%dintentstreamtrack_id%s%s%s",
		formatID, trackID, timestamp, appSecret)
	sum := md5.Sum([]byte(raw)) //nolint:gosec // required by the Qobuz API
	return hex.EncodeToString(sum[:])
}

// qobuzDirectFormatID maps a Navidrums quality onto a Qobuz format. Qobuz only
// accepts 5 (MP3 320), 6 (FLAC 16/44.1), 7 (FLAC <=24/96) and 27 (FLAC <=24/192),
// and downgrades on its own when the release is not available at that tier.
func qobuzDirectFormatID(quality string) int {
	switch quality {
	case constants.QualityHiResLossless:
		return 27
	case constants.QualityLossless:
		return 6
	case constants.QualityHigh, constants.QualityLow:
		return 5
	default:
		return 6
	}
}

// resolveTrackID maps an ISRC onto a Qobuz track ID, only trusting a search
// result whose ISRC matches exactly.
func (p *QobuzDirectProvider) resolveTrackID(ctx context.Context, trackID string, isrc string) string {
	if isrc == "" {
		return trackID
	}

	params := url.Values{}
	params.Set("query", isrc)
	params.Set("limit", "10")

	var resp QobuzSearchData
	if err := p.authedGet(ctx, "track/search", params, &resp); err != nil {
		return trackID
	}

	for _, item := range resp.Tracks.Items {
		if item.ID > 0 && strings.EqualFold(item.ISRC, isrc) {
			return strconv.Itoa(item.ID)
		}
	}

	return trackID
}

func (p *QobuzDirectProvider) GetSimilarAlbums(ctx context.Context, id string) ([]domain.Album, error) {
	params := url.Values{}
	params.Set("album_id", id)

	var resp QobuzSuggestedAlbumsResponse
	if err := p.authedGet(ctx, "album/suggest", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get similar albums failed: %w", err)
	}

	albums := make([]domain.Album, 0, len(resp.Albums.Items))
	for i := range resp.Albums.Items {
		albums = append(albums, resp.Albums.Items[i].ToDomain())
	}
	return albums, nil
}

func (p *QobuzDirectProvider) GetSimilarArtists(ctx context.Context, id string) ([]domain.Artist, error) {
	params := url.Values{}
	params.Set("artist_id", id)
	params.Set("limit", strconv.Itoa(similarArtistsLimit))
	params.Set("offset", "0")

	var resp QobuzSimilarArtistsResponse
	if err := p.authedGet(ctx, "artist/getSimilarArtists", params, &resp); err != nil {
		return nil, fmt.Errorf("qobuz get similar artists failed: %w", err)
	}

	artists := make([]domain.Artist, 0, len(resp.Artists.Items))
	for i := range resp.Artists.Items {
		artists = append(artists, resp.Artists.Items[i].ToDomain())
	}
	return artists, nil
}

func (p *QobuzDirectProvider) GetLyrics(ctx context.Context, trackID string) (string, string, error) {
	return "", "", ErrQobuzNotSupported
}

// GetRecommendations has no equivalent on the Qobuz API that takes a track ID:
// the web player uses a POST to dynamic/suggest built from listening history.
// Returning an empty list rather than an error keeps this optional panel quiet
// instead of surfacing a failure for something Qobuz simply does not offer.
func (p *QobuzDirectProvider) GetRecommendations(ctx context.Context, id string) ([]domain.CatalogTrack, error) {
	return nil, nil
}

// authToken returns a cached auth token, logging in when there is none or when
// the caller reports the current one was rejected.
func (p *QobuzDirectProvider) authToken(ctx context.Context, force bool) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !force && p.token != "" {
		return p.token, nil
	}
	if !p.creds.CanAuthenticate() {
		return "", ErrQobuzCredentialsMissing
	}

	// A token supplied directly is all we have: there is no password to trade
	// for a fresh one, so a rejected token is a configuration problem.
	if p.creds.UsesAuthToken() {
		if force {
			return "", ErrQobuzTokenRejected
		}
		p.token = p.creds.AuthToken
		return p.token, nil
	}

	params := url.Values{}
	params.Set("email", p.creds.Email)
	params.Set("password", p.creds.PasswordMD5)
	params.Set("app_id", p.creds.AppID)

	var resp QobuzLoginResponse
	if err := p.get(ctx, "user/login", params, "", &resp); err != nil {
		return "", fmt.Errorf("qobuz login failed: %w", err)
	}

	if resp.UserAuthToken == "" {
		return "", errors.New("qobuz login returned no auth token")
	}
	if resp.User.Credential.Parameters == nil {
		return "", ErrQobuzIneligible
	}

	p.token = resp.UserAuthToken
	return p.token, nil
}

// authedGet performs a request with the account's auth token, retrying once
// with a fresh token if Qobuz rejects the cached one.
func (p *QobuzDirectProvider) authedGet(ctx context.Context, endpoint string, params url.Values, target interface{}) error {
	token, err := p.authToken(ctx, false)
	if err != nil {
		return err
	}

	err = p.get(ctx, endpoint, params, token, target)
	if statusCodeOf(err) != http.StatusUnauthorized {
		return err
	}

	token, loginErr := p.authToken(ctx, true)
	if loginErr != nil {
		return loginErr
	}

	return p.get(ctx, endpoint, params, token, target)
}

func (p *QobuzDirectProvider) get(ctx context.Context, endpoint string, params url.Values, token string, target interface{}) error {
	query := url.Values{}
	for key, values := range params {
		query[key] = values
	}
	query.Set("app_id", p.creds.AppID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/%s?%s", p.BaseURL, endpoint, query.Encode()), nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", defaultProviderUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-App-Id", p.creds.AppID)
	if token != "" {
		req.Header.Set("X-User-Auth-Token", token)
	}

	resp, err := p.client.Do(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return &apiStatusError{StatusCode: resp.StatusCode, Status: resp.Status, Body: readErrorBody(resp.Body)}
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// openStreamURL fetches a signed Qobuz CDN URL and returns the still-open
// response. Callers own the body.
func (p *QobuzDirectProvider) openStreamURL(ctx context.Context, streamURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultProviderUserAgent)

	resp, err := streamHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stream: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("stream fetch failed: %s", resp.Status)
	}

	return resp, nil
}

var _ Provider = (*QobuzDirectProvider)(nil)
