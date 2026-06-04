// Package notifications provides a multi-channel notification dispatcher that
// sends alerts to Slack, Microsoft Teams, PagerDuty, email, and generic webhook
// endpoints. Each channel is independently configurable with a minimum priority
// threshold so operators can, for example, route only critical alerts to
// PagerDuty while sending all severities to Slack.
//
// The dispatcher fans out to every enabled channel whose MinPriority threshold
// is met by the notification. Delivery is synchronous within Send — the caller
// receives the first error encountered (if any) so upstream code can log or
// retry as appropriate. For fire-and-forget delivery, wrap Send in a goroutine.
//
// Channel payloads follow each provider's native format:
//   - Slack: Block Kit (section + mrkdwn)
//   - Teams: MessageCard (legacy connector format, widest compatibility)
//   - PagerDuty: Events API v2 trigger
//   - Email: plain-text via net/smtp (SMTP_HOST, SMTP_PORT, SMTP_FROM env vars)
//   - Webhook: raw JSON POST (same shape as the Notification struct)
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Channel and Priority enumerations
// ---------------------------------------------------------------------------

// Channel identifies the delivery backend for a notification.
type Channel string

const (
	ChannelSlack     Channel = "slack"
	ChannelTeams     Channel = "teams"
	ChannelPagerDuty Channel = "pagerduty"
	ChannelEmail     Channel = "email"
	ChannelWebhook   Channel = "webhook" // generic JSON POST
)

// Priority controls routing: each ChannelConfig declares a MinPriority and
// only notifications at or above that level are dispatched to the channel.
type Priority string

const (
	PriorityLow      Priority = "low"
	PriorityMedium   Priority = "medium"
	PriorityHigh     Priority = "high"
	PriorityCritical Priority = "critical"
)

// priorityRank maps a Priority to a numeric rank for threshold comparison.
// Higher rank = more severe.
func priorityRank(p Priority) int {
	switch p {
	case PriorityLow:
		return 0
	case PriorityMedium:
		return 1
	case PriorityHigh:
		return 2
	case PriorityCritical:
		return 3
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// Notification is the unit of work dispatched through the system.
type Notification struct {
	ID        string            `json:"id"`
	Channel   Channel           `json:"channel"`
	Priority  Priority          `json:"priority"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// ChannelConfig describes one delivery target. Enabled must be true for the
// channel to receive notifications. MinPriority acts as a high-pass filter:
// only notifications with Priority >= MinPriority are dispatched.
type ChannelConfig struct {
	Channel     Channel  `json:"channel"`
	Enabled     bool     `json:"enabled"`
	Endpoint    string   `json:"endpoint"`     // webhook URL, email address, etc.
	Token       string   `json:"token"`        // API token (Slack bot token, PagerDuty routing key)
	MinPriority Priority `json:"min_priority"` // only send at or above this priority
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

// Dispatcher routes Notification values to all configured channels whose
// priority threshold is met. It keeps a bounded recent-history buffer for
// debugging via RecentNotifications.
type Dispatcher struct {
	channels []ChannelConfig
	client   *http.Client
	mu       sync.RWMutex
	sent     []Notification // recent history (bounded by maxHistory)
}

const maxHistory = 500

// NewDispatcher returns a ready-to-use Dispatcher. The supplied channel
// configs are stored by value; mutating the slice after construction has no
// effect.
func NewDispatcher(channels []ChannelConfig) *Dispatcher {
	chs := make([]ChannelConfig, len(channels))
	copy(chs, channels)
	return &Dispatcher{
		channels: chs,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		sent: make([]Notification, 0, maxHistory),
	}
}

// Send routes n to every enabled channel whose MinPriority threshold is met.
// The Notification.ID is assigned automatically if empty. The first delivery
// error short-circuits and is returned; remaining channels are still attempted
// so a single broken channel does not block the rest.
func (d *Dispatcher) Send(ctx context.Context, n Notification) error {
	if n.ID == "" {
		n.ID = uuid.New().String()
	}
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}

	var firstErr error
	for _, cfg := range d.channels {
		if !cfg.Enabled {
			continue
		}
		if priorityRank(n.Priority) < priorityRank(cfg.MinPriority) {
			continue
		}

		sent := n
		sent.Channel = cfg.Channel

		var err error
		switch cfg.Channel {
		case ChannelSlack:
			err = d.sendSlack(ctx, sent, cfg)
		case ChannelTeams:
			err = d.sendTeams(ctx, sent, cfg)
		case ChannelPagerDuty:
			err = d.sendPagerDuty(ctx, sent, cfg)
		case ChannelEmail:
			err = d.sendEmail(ctx, sent, cfg)
		case ChannelWebhook:
			err = d.sendWebhook(ctx, sent, cfg)
		default:
			err = fmt.Errorf("notifications: unknown channel %q", cfg.Channel)
		}

		if err != nil && firstErr == nil {
			firstErr = err
		}

		// Record regardless of error so the history shows attempted deliveries.
		d.recordSent(sent)
	}
	return firstErr
}

// recordSent appends n to the bounded history buffer.
func (d *Dispatcher) recordSent(n Notification) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, n)
	if len(d.sent) > maxHistory {
		// Trim oldest entries when the buffer overflows.
		d.sent = d.sent[len(d.sent)-maxHistory:]
	}
}

// RecentNotifications returns the most recent limit notifications (most recent
// first). If limit <= 0 or exceeds the buffer size, the full buffer is
// returned.
func (d *Dispatcher) RecentNotifications(limit int) []Notification {
	d.mu.RLock()
	defer d.mu.RUnlock()
	n := len(d.sent)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]Notification, limit)
	for i := 0; i < limit; i++ {
		out[i] = d.sent[n-1-i]
	}
	return out
}

// ---------------------------------------------------------------------------
// Channel-specific senders
// ---------------------------------------------------------------------------

// sendSlack posts a Block Kit message to a Slack incoming webhook URL.
//
// Payload shape:
//
//	{"text": "<title>", "blocks": [{"type": "section", "text": {"type": "mrkdwn", "text": "<body>"}}]}
func (d *Dispatcher) sendSlack(ctx context.Context, n Notification, cfg ChannelConfig) error {
	payload := map[string]any{
		"text": n.Title,
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*%s*\n%s", n.Title, n.Body),
				},
			},
		},
	}
	return d.postJSON(ctx, cfg.Endpoint, payload, nil)
}

// sendTeams posts a MessageCard to a Microsoft Teams incoming webhook.
//
// Payload shape (legacy connector format — widest compatibility):
//
//	{"@type": "MessageCard", "@context": "...", "summary": "<title>",
//	 "sections": [{"activityTitle": "<title>", "text": "<body>"}]}
func (d *Dispatcher) sendTeams(ctx context.Context, n Notification, cfg ChannelConfig) error {
	payload := map[string]any{
		"@type":    "MessageCard",
		"@context": "http://schema.org/extensions",
		"summary":  n.Title,
		"sections": []map[string]any{
			{
				"activityTitle": n.Title,
				"text":          n.Body,
			},
		},
	}
	return d.postJSON(ctx, cfg.Endpoint, payload, nil)
}

// sendPagerDuty triggers an incident via the PagerDuty Events API v2.
//
// Payload shape:
//
//	{"routing_key": "<token>", "event_action": "trigger",
//	 "payload": {"summary": "<title>", "severity": "<mapped>", "source": "bodh"}}
func (d *Dispatcher) sendPagerDuty(ctx context.Context, n Notification, cfg ChannelConfig) error {
	payload := map[string]any{
		"routing_key":  cfg.Token,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":  n.Title,
			"severity": mapPagerDutySeverity(n.Priority),
			"source":   "bodh",
		},
	}
	headers := map[string]string{
		"X-Routing-Key": cfg.Token,
	}
	return d.postJSON(ctx, "https://events.pagerduty.com/v2/enqueue", payload, headers)
}

// mapPagerDutySeverity converts a Bodh Priority to a PagerDuty severity string.
// PagerDuty accepts: info, warning, error, critical.
func mapPagerDutySeverity(p Priority) string {
	switch p {
	case PriorityLow:
		return "info"
	case PriorityMedium:
		return "warning"
	case PriorityHigh:
		return "error"
	case PriorityCritical:
		return "critical"
	default:
		return "info"
	}
}

// sendEmail sends a plain-text email via SMTP. Configuration is drawn from
// environment variables:
//   - SMTP_HOST: SMTP server hostname (e.g. "smtp.example.com")
//   - SMTP_PORT: SMTP server port (default "587")
//   - SMTP_FROM: sender address (e.g. "bodh@example.com")
//
// cfg.Endpoint is the recipient address. cfg.Token, if non-empty, is used as
// the SMTP password for PLAIN auth (username = SMTP_FROM).
func (d *Dispatcher) sendEmail(_ context.Context, n Notification, cfg ChannelConfig) error {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		return fmt.Errorf("notifications: SMTP_HOST not set")
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		return fmt.Errorf("notifications: SMTP_FROM not set")
	}
	to := cfg.Endpoint

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: [bodh] %s\r\n\r\n%s\r\n",
		from, to, n.Title, n.Body)

	addr := fmt.Sprintf("%s:%s", host, port)

	var auth smtp.Auth
	if cfg.Token != "" {
		auth = smtp.PlainAuth("", from, cfg.Token, host)
	}

	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
}

// sendWebhook posts the raw Notification as JSON to a generic webhook endpoint.
func (d *Dispatcher) sendWebhook(ctx context.Context, n Notification, cfg ChannelConfig) error {
	return d.postJSON(ctx, cfg.Endpoint, n, nil)
}

// ---------------------------------------------------------------------------
// HTTP helper
// ---------------------------------------------------------------------------

// postJSON marshals v to JSON and POSTs it to url. Extra headers (if non-nil)
// are set on the request. Returns an error on transport failure or non-2xx
// response.
func (d *Dispatcher) postJSON(ctx context.Context, url string, v any, headers map[string]string) error {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("notifications: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notifications: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, val := range headers {
		req.Header.Set(k, val)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("notifications: post %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notifications: non-2xx status %d from %s", resp.StatusCode, url)
	}
	return nil
}
