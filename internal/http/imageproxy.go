package httpapp

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// placeholderImagePath is served whenever there is no artwork to show.
const placeholderImagePath = "/static/img/placeholder.svg"

// imageProxyPath serves catalog artwork from this app's own origin.
const imageProxyPath = "/img"

// allowedImageHosts restricts what the proxy will fetch. Without an allowlist
// this endpoint would be an open proxy usable to reach anything the server can
// see, including private network addresses.
var allowedImageHosts = map[string]bool{
	"static.qobuz.com": true,
}

// imageProxyClient is separate from the catalog clients: artwork is small, and
// a slow CDN should not hold a connection open for long.
var imageProxyClient = &http.Client{Timeout: 20 * time.Second}

// ProxiedImageURL rewrites a catalog artwork URL to be served through this app.
//
// Browsers with tracking protection or an ad blocker refuse to load images
// straight from the catalog CDNs (net::ERR_BLOCKED_BY_CLIENT), which left every
// cover and portrait broken even though the URLs were correct. Serving them
// from our own origin sidesteps that, and stops the browser telling Qobuz what
// the user is browsing.
func ProxiedImageURL(raw string) string {
	if raw == "" {
		return placeholderImagePath
	}

	parsed, err := url.Parse(raw)
	if err != nil || !allowedImageHosts[parsed.Hostname()] {
		// Anything unrecognised is passed through untouched rather than
		// proxied, so a new CDN degrades to today's behaviour.
		return raw
	}

	return imageProxyPath + "?url=" + url.QueryEscape(raw)
}

// ImageProxy fetches catalog artwork and streams it back to the browser.
func (h *Handler) ImageProxy(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	if raw == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || !allowedImageHosts[parsed.Hostname()] {
		http.Error(w, "image host not allowed", http.StatusForbidden)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, parsed.String(), nil)
	if err != nil {
		http.Error(w, "bad image request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", imageProxyUserAgent)

	resp, err := imageProxyClient.Do(req)
	if err != nil {
		http.Redirect(w, r, placeholderImagePath, http.StatusFound)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		http.Redirect(w, r, placeholderImagePath, http.StatusFound)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		http.Redirect(w, r, placeholderImagePath, http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", contentType)
	// Artwork at a given URL never changes, so let the browser keep it.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if length := resp.Header.Get("Content-Length"); length != "" {
		w.Header().Set("Content-Length", length)
	}

	_, _ = io.Copy(w, resp.Body)
}

const imageProxyUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
