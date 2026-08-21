package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// FlexCover handles flexible cover image formats from the API
type FlexCover []string

// UnmarshalJSON implements custom JSON unmarshaling for FlexCover
func (f *FlexCover) UnmarshalJSON(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	// Handle string format
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = []string{s}
		return nil
	}

	// Handle array format with objects
	if data[0] == '[' {
		var items []struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		var urls []string
		for _, item := range items {
			urls = append(urls, item.URL)
		}
		*f = urls
		return nil
	}

	// Handle object format
	if data[0] == '{' {
		var item struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		*f = []string{item.URL}
		return nil
	}

	return nil
}

// formatID converts various ID types to string
func formatID(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%.0f", val)
	case json.Number:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// multiSegmentReader implements io.ReadCloser for segmented DASH streams
type multiSegmentReader struct {
	ctx      context.Context
	currBody io.ReadCloser
	client   *http.Client
	urls     []string
	currIdx  int
}

func (r *multiSegmentReader) Read(p []byte) (n int, err error) {
	if r.currBody == nil {
		if r.currIdx >= len(r.urls) {
			return 0, io.EOF
		}

		// Check context before fetching segment
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		default:
		}

		// Fetch next segment
		var req *http.Request
		req, err = http.NewRequestWithContext(r.ctx, "GET", r.urls[r.currIdx], nil)
		if err != nil {
			return 0, err
		}
		var resp *http.Response
		resp, err = r.client.Do(req)
		if err != nil {
			return 0, err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return 0, fmt.Errorf("segment fetch failed (%d): %s", r.currIdx, resp.Status)
		}
		r.currBody = resp.Body
		r.currIdx++
	}

	n, err = r.currBody.Read(p)
	if err == io.EOF {
		_ = r.currBody.Close()
		r.currBody = nil
		// Check context before recursive call
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		default:
			return r.Read(p) // recursive call to next segment
		}
	}
	return n, err
}

func (r *multiSegmentReader) Close() error {
	if r.currBody != nil {
		return r.currBody.Close()
	}
	return nil
}

// apiStatusError is returned by provider HTTP helpers when the catalog API
// answers with a non-200 status. Callers can inspect StatusCode to tell a
// missing route (404) apart from an upstream failure. Body carries a snippet of
// the response, which is usually where the API explains the refusal.
type apiStatusError struct {
	Status     string
	Body       string
	StatusCode int
}

func (e *apiStatusError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("API request failed: %s: %s", e.Status, e.Body)
	}
	return fmt.Sprintf("API request failed: %s", e.Status)
}

// maxErrorBody caps how much of a failed response is kept for the error message.
const maxErrorBody = 200

// readErrorBody returns a short, single-line snippet of a failed response body.
func readErrorBody(r io.Reader) string {
	body, err := io.ReadAll(io.LimitReader(r, maxErrorBody))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.ReplaceAll(string(body), "\n", " "))
}

// statusCodeOf reports the HTTP status carried by err, or 0 when err did not
// come from a catalog API response.
func statusCodeOf(err error) int {
	var statusErr *apiStatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode
	}
	return 0
}

// multiError reports every provider failure on a single line while still
// matching errors.Is against each underlying cause. A fallback chain that kept
// only the last error hid why the *primary* provider failed, which is usually
// the one the user configured and cares about.
type multiError struct {
	errs []error
}

func (m *multiError) Error() string {
	parts := make([]string, 0, len(m.errs))
	for _, err := range m.errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}

func (m *multiError) Unwrap() []error {
	return m.errs
}

// joinErrors collapses provider failures into one error, or returns the single
// error unchanged when there is only one.
func joinErrors(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return &multiError{errs: errs}
	}
}
