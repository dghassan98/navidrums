package catalog

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

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

// parseYear extracts a four digit year from the start of a date string,
// returning 0 when there is none. Qobuz dates arrive in several shapes
// (YYYY, YYYY-MM-DD, RFC3339), and only the year is ever tagged.
func parseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	var year int
	if _, err := fmt.Sscanf(date[:4], "%d", &year); err != nil {
		return 0
	}
	return year
}
