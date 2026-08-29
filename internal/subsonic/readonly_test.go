package subsonic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// writeEndpoints are the Subsonic calls that modify the server or its library.
// None of them may ever be reachable from this package.
var writeEndpoints = []string{
	"createPlaylist", "updatePlaylist", "deletePlaylist",
	"createUser", "updateUser", "deleteUser", "changePassword",
	"createShare", "updateShare", "deleteShare",
	"createPodcastChannel", "deletePodcastChannel", "deletePodcastEpisode",
	"setRating", "star", "unstar", "scrobble",
	"createInternetRadioStation", "updateInternetRadioStation", "deleteInternetRadioStation",
	"savePlayQueue", "saveQueue",
}

// startScan is deliberately absent from that list. It is the one call that
// changes server state, and only by asking it to re-read the library — it does
// not modify a single file. Without it, tags written by the cleanup would stay
// invisible until the server happened to rescan on its own.

// TestClientOnlyIssuesReadOnlyRequests is the guarantee the whole feature
// rests on: the music library is never modified by Navidrums. It is enforced
// here rather than promised in a comment, by exercising every public call and
// asserting on what actually went over the wire.
func TestClientOnlyIssuesReadOnlyRequests(t *testing.T) {
	var mu sync.Mutex
	var calls []struct{ method, path string }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, struct{ method, path string }{r.Method, r.URL.Path})
		mu.Unlock()
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","version":"1.16.1","searchResult3":{"song":[]}}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "user", "pass")
	ctx := context.Background()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := c.Probe(ctx); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if err := c.Songs(ctx, func([]Song) error { return nil }); err != nil {
		t.Fatalf("Songs: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(calls) == 0 {
		t.Fatal("no requests were made; the test proves nothing")
	}

	for _, call := range calls {
		if call.method != http.MethodGet {
			t.Errorf("%s %s: only GET may be used against the library", call.method, call.path)
		}
		for _, w := range writeEndpoints {
			if strings.Contains(strings.ToLower(call.path), strings.ToLower(w)) {
				t.Errorf("called a write endpoint: %s", call.path)
			}
		}
	}
}

// TestPasswordIsNeverSentInTheClear covers the other half of handing over
// credentials: Subsonic's token scheme means the password itself must not
// appear in any request.
func TestPasswordIsNeverSentInTheClear(t *testing.T) {
	const password = "T0pS3cretValue"
	var mu sync.Mutex
	var urls []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		urls = append(urls, r.URL.String())
		mu.Unlock()
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok","searchResult3":{"song":[]}}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "user", password)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, u := range urls {
		if strings.Contains(u, password) {
			t.Errorf("the password appeared in a request URL: %s", u)
		}
		if !strings.Contains(u, "t=") || !strings.Contains(u, "s=") {
			t.Errorf("request %s is missing the salted token", u)
		}
	}
}

// TestSaltIsPerRequest keeps the token scheme meaningful: a fixed salt would
// make the token a reusable password equivalent.
func TestSaltIsPerRequest(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Query().Get("s")] = true
		mu.Unlock()
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"ok"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "user", "pass")
	for i := 0; i < 3; i++ {
		if err := c.Ping(context.Background()); err != nil {
			t.Fatalf("Ping: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Errorf("got %d distinct salts across 3 requests, want 3", len(seen))
	}
}

// TestSubsonicErrorsAreNotMistakenForSuccess covers the protocol's trap:
// failures arrive with HTTP 200 and an error object in the body.
func TestSubsonicErrorsAreNotMistakenForSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"subsonic-response":{"status":"failed","error":{"code":40,"message":"Wrong username or password"}}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "user", "wrong")
	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("a failed auth returned no error despite HTTP 200")
	}
	if !strings.Contains(err.Error(), "Wrong username or password") {
		t.Errorf("error did not carry the server's reason: %v", err)
	}
}

func TestUnconfiguredClientIsNotAnError(t *testing.T) {
	// The library index is optional; an unconfigured one must be
	// distinguishable from a broken one.
	c := NewClient("", "", "")
	if c.Configured() {
		t.Error("an empty client reported itself configured")
	}
	if err := c.Ping(context.Background()); err != ErrNotConfigured {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}
