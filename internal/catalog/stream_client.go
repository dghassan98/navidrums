package catalog

import (
	"io"
	"net/http"
	"time"
)

// streamHTTPClient is used for audio bodies only.
//
// http.Client.Timeout covers the whole exchange including reading the body, so
// the 20 second timeout that suits a JSON API call kills any download that
// takes longer than that. A lossless track routinely does. There is no overall
// deadline here; the request context still cancels the transfer, and the
// transport timeouts below stop a connection hanging before data flows.
var streamHTTPClient = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
	},
}

// sizedStream carries the total size of an audio body alongside it, so the
// downloader can report real progress. Providers hand back a plain
// io.ReadCloser, so callers type-assert for StreamSizer rather than the
// Provider interface changing shape.
type sizedStream struct {
	io.ReadCloser
	size int64
}

// Size returns the total number of bytes expected, or 0 when the server did
// not say (a chunked response, for example).
func (s *sizedStream) Size() int64 {
	return s.size
}

// StreamSizer is implemented by streams that know their total size.
type StreamSizer interface {
	Size() int64
}

// withSize wraps a response body so its Content-Length travels with it.
func withSize(body io.ReadCloser, contentLength int64) io.ReadCloser {
	if contentLength <= 0 {
		return body
	}
	return &sizedStream{ReadCloser: body, size: contentLength}
}
