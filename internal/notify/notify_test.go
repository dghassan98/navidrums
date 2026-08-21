package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestIsDiscordWebhook(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"discord webhook", "https://discord.com/api/webhooks/123/abc", true},
		{"ptb subdomain", "https://ptb.discord.com/api/webhooks/123/abc", true},
		{"legacy discordapp domain", "https://discordapp.com/api/webhooks/123/abc", true},
		{"discord but not a webhook", "https://discord.com/channels/123", false},
		{"apprise endpoint", "https://apprise.example.test/notify/navidrums", false},
		{"lookalike host", "https://notdiscord.com.evil.test/api/webhooks/1/a", false},
		{"garbage", "://", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDiscordWebhook(tt.url); got != tt.want {
				t.Errorf("IsDiscordWebhook(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestEventDescription(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name:  "track with artist and album",
			event: Event{JobType: "track", Title: "Xtal", Artist: "Aphex Twin", Album: "Selected Ambient Works"},
			want:  "TRACK: Xtal — Aphex Twin — Selected Ambient Works",
		},
		{
			name:  "album repeated as title is not duplicated",
			event: Event{JobType: "album", Title: "Discovery", Artist: "Daft Punk", Album: "Discovery"},
			want:  "ALBUM: Discovery — Daft Punk",
		},
		{
			name:  "failure appends the reason",
			event: Event{JobType: "track", Title: "Xtal", Status: StatusFailed, Error: "boom"},
			want:  "TRACK: Xtal\nboom",
		},
		{
			name:  "no details still says something",
			event: Event{JobType: "track"},
			want:  "TRACK: (no track details)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.event.Description(); got != tt.want {
				t.Errorf("Description() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEventSummary(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusCompleted, "Download finished"},
		{StatusFailed, "Download failed"},
		{StatusCancelled, "Download cancelled"},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := (Event{Status: tt.status}).Summary(); got != tt.want {
				t.Errorf("Summary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPayloadShape(t *testing.T) {
	event := Event{Status: StatusCompleted, JobType: "track", Title: "Xtal", Artist: "Aphex Twin"}

	t.Run("discord uses embeds", func(t *testing.T) {
		raw, err := BuildPayload("https://discord.com/api/webhooks/1/a", event)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("payload is not valid json: %v", err)
		}
		if _, ok := payload["embeds"]; !ok {
			t.Errorf("discord payload has no embeds: %s", raw)
		}
	})

	t.Run("apprise uses title and body", func(t *testing.T) {
		raw, err := BuildPayload("https://apprise.example.test/notify/x", event)
		if err != nil {
			t.Fatalf("BuildPayload failed: %v", err)
		}

		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("payload is not valid json: %v", err)
		}
		for _, key := range []string{"title", "body", "type"} {
			if _, ok := payload[key]; !ok {
				t.Errorf("apprise payload missing %q: %s", key, raw)
			}
		}
		if payload["type"] != "success" {
			t.Errorf("type = %v, want success", payload["type"])
		}
	})
}

func TestNotifierEnabled(t *testing.T) {
	tests := []struct {
		name     string
		provider URLProvider
		want     bool
	}{
		{"configured", func() string { return "https://example.test/hook" }, true},
		{"blank", func() string { return "   " }, false},
		{"empty", func() string { return "" }, false},
		{"nil provider", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New(tt.provider, nil).Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotifierSend(t *testing.T) {
	var mu sync.Mutex
	var body string

	mux := http.NewServeMux()
	mux.HandleFunc("/hook", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(raw)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	notifier := New(func() string { return server.URL + "/hook" }, nil)

	t.Run("delivers the event", func(t *testing.T) {
		err := notifier.Send(context.Background(), server.URL+"/hook",
			Event{Status: StatusCompleted, JobType: "track", Title: "Xtal"})
		if err != nil {
			t.Fatalf("Send failed: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if !strings.Contains(body, "Xtal") {
			t.Errorf("delivered body = %q, want it to mention the track", body)
		}
	})

	t.Run("reports a rejecting webhook", func(t *testing.T) {
		err := notifier.Send(context.Background(), server.URL+"/broken", Event{Status: StatusFailed})
		if err == nil {
			t.Error("Send succeeded against a failing webhook")
		}
	})

	t.Run("notify swallows failures", func(t *testing.T) {
		// A broken webhook must never surface into the download path.
		broken := New(func() string { return server.URL + "/broken" }, nil)
		broken.Notify(context.Background(), Event{Status: StatusFailed})
	})
}
