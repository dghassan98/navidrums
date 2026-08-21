// Package notify sends download notifications to a webhook.
//
// It targets two shapes with one URL: a Discord webhook, and an Apprise API
// endpoint that fans out to whatever Apprise is configured with. The shape is
// chosen from the URL so there is nothing extra to configure.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Status is the outcome being reported.
type Status string

const (
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Event describes something worth telling the user about.
type Event struct {
	Status  Status
	JobType string
	Title   string
	Artist  string
	Album   string
	Error   string
}

// Summary is the one-line description used as a notification title.
func (e Event) Summary() string {
	switch e.Status {
	case StatusCompleted:
		return "Download finished"
	case StatusFailed:
		return "Download failed"
	case StatusCancelled:
		return "Download cancelled"
	default:
		return "Download update"
	}
}

// Description is the body: what was downloaded, and why it failed if it did.
func (e Event) Description() string {
	var parts []string

	if e.Title != "" {
		parts = append(parts, e.Title)
	}
	if e.Artist != "" {
		parts = append(parts, e.Artist)
	}
	if e.Album != "" && e.Album != e.Title {
		parts = append(parts, e.Album)
	}

	description := strings.Join(parts, " — ")
	if description == "" {
		description = "(no track details)"
	}
	if e.JobType != "" {
		description = strings.ToUpper(e.JobType) + ": " + description
	}
	if e.Error != "" {
		description += "\n" + e.Error
	}

	return description
}

// colour is the Discord embed colour for the outcome.
func (e Event) colour() int {
	switch e.Status {
	case StatusCompleted:
		return 0x2ecc71
	case StatusFailed:
		return 0xe74c3c
	case StatusCancelled:
		return 0xf1c40f
	default:
		return 0x95a5a6
	}
}

// appriseType maps onto Apprise's notification levels.
func (e Event) appriseType() string {
	switch e.Status {
	case StatusCompleted:
		return "success"
	case StatusFailed:
		return "failure"
	default:
		return "warning"
	}
}

// URLProvider resolves the webhook URL at send time, so a change in Settings
// applies without a restart.
type URLProvider func() string

// Logger is the subset of the app logger this package needs.
type Logger interface {
	Debug(msg string, args ...any)
	Error(msg string, args ...any)
}

// Notifier posts events to the configured webhook.
type Notifier struct {
	client  *http.Client
	url     URLProvider
	logger  Logger
	timeout time.Duration
}

// New builds a Notifier. A nil or empty URLProvider disables notifications.
func New(urlProvider URLProvider, logger Logger) *Notifier {
	return &Notifier{
		client:  &http.Client{Timeout: 15 * time.Second},
		url:     urlProvider,
		logger:  logger,
		timeout: 15 * time.Second,
	}
}

// Enabled reports whether a webhook is configured.
func (n *Notifier) Enabled() bool {
	return n != nil && n.url != nil && strings.TrimSpace(n.url()) != ""
}

// Notify sends the event, logging and swallowing any failure: a notification
// problem must never fail or delay a download.
func (n *Notifier) Notify(ctx context.Context, event Event) {
	if !n.Enabled() {
		return
	}

	if err := n.Send(ctx, strings.TrimSpace(n.url()), event); err != nil && n.logger != nil {
		n.logger.Error("Failed to send notification", "status", event.Status, "error", err)
	}
}

// Send posts one event to an explicit webhook URL and reports the outcome. Test
// buttons use this directly so they can show what went wrong.
func (n *Notifier) Send(ctx context.Context, webhookURL string, event Event) error {
	payload, err := BuildPayload(webhookURL, event)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("invalid webhook url: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Discord answers 204 with no body; Apprise answers 200.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}

	return nil
}

// IsDiscordWebhook reports whether the URL is a Discord webhook, which takes a
// different payload from an Apprise endpoint.
func IsDiscordWebhook(webhookURL string) bool {
	parsed, err := url.Parse(webhookURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, "discord.com") && !strings.HasSuffix(host, "discordapp.com") {
		return false
	}

	return strings.Contains(parsed.Path, "/api/webhooks/")
}

// BuildPayload renders the event into the body the target expects.
func BuildPayload(webhookURL string, event Event) ([]byte, error) {
	if IsDiscordWebhook(webhookURL) {
		return json.Marshal(map[string]any{
			"username": "Navidrums",
			"embeds": []map[string]any{{
				"title":       event.Summary(),
				"description": event.Description(),
				"color":       event.colour(),
			}},
		})
	}

	// Apprise API: it accepts title/body/type and handles delivery itself.
	return json.Marshal(map[string]any{
		"title": event.Summary(),
		"body":  event.Description(),
		"type":  event.appriseType(),
	})
}
