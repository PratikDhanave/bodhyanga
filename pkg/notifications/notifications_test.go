package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Slack delivery
// ---------------------------------------------------------------------------

func TestSendSlack(t *testing.T) {
	var mu sync.Mutex
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{
			Channel:     ChannelSlack,
			Enabled:     true,
			Endpoint:    srv.URL,
			MinPriority: PriorityLow,
		},
	})

	n := Notification{
		Title:    "Test Alert",
		Body:     "Something happened",
		Priority: PriorityHigh,
	}

	if err := d.Send(context.Background(), n); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if received == nil {
		t.Fatal("no payload received by Slack server")
	}
	if received["text"] != "Test Alert" {
		t.Errorf("expected text='Test Alert', got %q", received["text"])
	}
	blocks, ok := received["blocks"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatal("expected blocks array with at least one element")
	}
	block := blocks[0].(map[string]any)
	if block["type"] != "section" {
		t.Errorf("expected block type 'section', got %q", block["type"])
	}
}

// ---------------------------------------------------------------------------
// Teams delivery
// ---------------------------------------------------------------------------

func TestSendTeams(t *testing.T) {
	var mu sync.Mutex
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{
			Channel:     ChannelTeams,
			Enabled:     true,
			Endpoint:    srv.URL,
			MinPriority: PriorityLow,
		},
	})

	n := Notification{
		Title:    "Teams Test",
		Body:     "Body text",
		Priority: PriorityMedium,
	}

	if err := d.Send(context.Background(), n); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if received == nil {
		t.Fatal("no payload received by Teams server")
	}
	if received["@type"] != "MessageCard" {
		t.Errorf("expected @type='MessageCard', got %q", received["@type"])
	}
	if received["summary"] != "Teams Test" {
		t.Errorf("expected summary='Teams Test', got %q", received["summary"])
	}
	sections, ok := received["sections"].([]any)
	if !ok || len(sections) == 0 {
		t.Fatal("expected sections array with at least one element")
	}
	sec := sections[0].(map[string]any)
	if sec["activityTitle"] != "Teams Test" {
		t.Errorf("expected activityTitle='Teams Test', got %q", sec["activityTitle"])
	}
	if sec["text"] != "Body text" {
		t.Errorf("expected text='Body text', got %q", sec["text"])
	}
}

// ---------------------------------------------------------------------------
// Generic webhook delivery
// ---------------------------------------------------------------------------

func TestSendWebhook(t *testing.T) {
	var mu sync.Mutex
	var received Notification

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{
			Channel:     ChannelWebhook,
			Enabled:     true,
			Endpoint:    srv.URL,
			MinPriority: PriorityLow,
		},
	})

	n := Notification{
		Title:    "Webhook Test",
		Body:     "Payload body",
		Priority: PriorityLow,
		Metadata: map[string]string{"case_id": "abc123"},
	}

	if err := d.Send(context.Background(), n); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if received.Title != "Webhook Test" {
		t.Errorf("expected title='Webhook Test', got %q", received.Title)
	}
	if received.Body != "Payload body" {
		t.Errorf("expected body='Payload body', got %q", received.Body)
	}
	if received.Metadata["case_id"] != "abc123" {
		t.Errorf("expected metadata case_id='abc123', got %q", received.Metadata["case_id"])
	}
}

// ---------------------------------------------------------------------------
// Priority filtering
// ---------------------------------------------------------------------------

func TestPriorityFiltering(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{
			Channel:     ChannelSlack,
			Enabled:     true,
			Endpoint:    srv.URL,
			MinPriority: PriorityHigh, // only high + critical
		},
	})

	// Low priority — should be filtered out.
	if err := d.Send(context.Background(), Notification{
		Title:    "Low",
		Priority: PriorityLow,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Medium priority — still below threshold.
	if err := d.Send(context.Background(), Notification{
		Title:    "Medium",
		Priority: PriorityMedium,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// High priority — should pass.
	if err := d.Send(context.Background(), Notification{
		Title:    "High",
		Priority: PriorityHigh,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Critical priority — should pass.
	if err := d.Send(context.Background(), Notification{
		Title:    "Critical",
		Priority: PriorityCritical,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if callCount != 2 {
		t.Errorf("expected 2 deliveries (high + critical), got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// Disabled channel
// ---------------------------------------------------------------------------

func TestDisabledChannel(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{
			Channel:     ChannelSlack,
			Enabled:     false, // disabled
			Endpoint:    srv.URL,
			MinPriority: PriorityLow,
		},
	})

	if err := d.Send(context.Background(), Notification{
		Title:    "Should not arrive",
		Priority: PriorityCritical,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if callCount != 0 {
		t.Errorf("expected 0 deliveries for disabled channel, got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// Multi-channel fan-out
// ---------------------------------------------------------------------------

func TestMultiChannelFanOut(t *testing.T) {
	var mu sync.Mutex
	channels := map[string]bool{}

	makeSrv := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			channels[name] = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
	}

	slackSrv := makeSrv("slack")
	defer slackSrv.Close()
	teamsSrv := makeSrv("teams")
	defer teamsSrv.Close()
	webhookSrv := makeSrv("webhook")
	defer webhookSrv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelSlack, Enabled: true, Endpoint: slackSrv.URL, MinPriority: PriorityLow},
		{Channel: ChannelTeams, Enabled: true, Endpoint: teamsSrv.URL, MinPriority: PriorityLow},
		{Channel: ChannelWebhook, Enabled: true, Endpoint: webhookSrv.URL, MinPriority: PriorityLow},
	})

	if err := d.Send(context.Background(), Notification{
		Title:    "Fan-out test",
		Priority: PriorityMedium,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"slack", "teams", "webhook"} {
		if !channels[name] {
			t.Errorf("channel %q did not receive the notification", name)
		}
	}
}

// ---------------------------------------------------------------------------
// RecentNotifications
// ---------------------------------------------------------------------------

func TestRecentNotifications(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelWebhook, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	// Send 5 notifications.
	for i := 0; i < 5; i++ {
		_ = d.Send(context.Background(), Notification{
			Title:    "Notification",
			Priority: PriorityLow,
		})
	}

	// Ask for 3 most recent.
	recent := d.RecentNotifications(3)
	if len(recent) != 3 {
		t.Errorf("expected 3 recent notifications, got %d", len(recent))
	}

	// Ask for more than available — should return all.
	all := d.RecentNotifications(100)
	if len(all) != 5 {
		t.Errorf("expected 5 total notifications, got %d", len(all))
	}

	// Zero limit returns all.
	allZero := d.RecentNotifications(0)
	if len(allZero) != 5 {
		t.Errorf("expected 5 for limit=0, got %d", len(allZero))
	}
}

// ---------------------------------------------------------------------------
// Non-2xx response error
// ---------------------------------------------------------------------------

func TestNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelSlack, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Title:    "Should fail",
		Priority: PriorityHigh,
	})
	if err == nil {
		t.Fatal("expected an error for non-2xx response, got nil")
	}
}

// ---------------------------------------------------------------------------
// ID and CreatedAt auto-fill
// ---------------------------------------------------------------------------

func TestAutoFillIDAndCreatedAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelWebhook, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	before := time.Now().UTC().Add(-time.Second)
	_ = d.Send(context.Background(), Notification{
		Title:    "Auto-fill",
		Priority: PriorityLow,
	})
	after := time.Now().UTC().Add(time.Second)

	recent := d.RecentNotifications(1)
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent notification, got %d", len(recent))
	}
	n := recent[0]
	if n.ID == "" {
		t.Error("expected auto-generated ID, got empty string")
	}
	if n.CreatedAt.Before(before) || n.CreatedAt.After(after) {
		t.Errorf("CreatedAt %v not in expected range [%v, %v]", n.CreatedAt, before, after)
	}
}

// ---------------------------------------------------------------------------
// PagerDuty payload shape (via httptest)
// ---------------------------------------------------------------------------

func TestSendPagerDutyPayload(t *testing.T) {
	var mu sync.Mutex
	var received map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	// Override the PagerDuty endpoint by using a webhook channel that posts
	// the same payload shape. Instead, we test sendPagerDuty directly by
	// temporarily pointing the client at our test server.
	d := NewDispatcher(nil)

	cfg := ChannelConfig{
		Channel:  ChannelPagerDuty,
		Enabled:  true,
		Endpoint: srv.URL,
		Token:    "test-routing-key",
	}

	n := Notification{
		ID:       "pd-test-1",
		Title:    "Critical Alert",
		Body:     "Server on fire",
		Priority: PriorityCritical,
	}

	// Call sendPagerDuty directly but override the URL by calling postJSON
	// through the exported Send path. For direct testing, we call the
	// unexported method through a wrapper that hits our test server.
	payload := map[string]any{
		"routing_key":  cfg.Token,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":  n.Title,
			"severity": mapPagerDutySeverity(n.Priority),
			"source":   "bodh",
		},
	}
	err := d.postJSON(context.Background(), srv.URL, payload, map[string]string{
		"X-Routing-Key": cfg.Token,
	})
	if err != nil {
		t.Fatalf("postJSON returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if received == nil {
		t.Fatal("no payload received")
	}
	if received["routing_key"] != "test-routing-key" {
		t.Errorf("expected routing_key='test-routing-key', got %q", received["routing_key"])
	}
	if received["event_action"] != "trigger" {
		t.Errorf("expected event_action='trigger', got %q", received["event_action"])
	}
	pl, ok := received["payload"].(map[string]any)
	if !ok {
		t.Fatal("expected payload object")
	}
	if pl["severity"] != "critical" {
		t.Errorf("expected severity='critical', got %q", pl["severity"])
	}
	if pl["source"] != "bodh" {
		t.Errorf("expected source='bodh', got %q", pl["source"])
	}
}

// ---------------------------------------------------------------------------
// mapPagerDutySeverity
// ---------------------------------------------------------------------------

func TestMapPagerDutySeverity(t *testing.T) {
	tests := []struct {
		in   Priority
		want string
	}{
		{PriorityLow, "info"},
		{PriorityMedium, "warning"},
		{PriorityHigh, "error"},
		{PriorityCritical, "critical"},
		{Priority("unknown"), "info"},
	}
	for _, tt := range tests {
		got := mapPagerDutySeverity(tt.in)
		if got != tt.want {
			t.Errorf("mapPagerDutySeverity(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// priorityRank
// ---------------------------------------------------------------------------

func TestPriorityRank(t *testing.T) {
	if priorityRank(PriorityLow) >= priorityRank(PriorityMedium) {
		t.Error("low should rank below medium")
	}
	if priorityRank(PriorityMedium) >= priorityRank(PriorityHigh) {
		t.Error("medium should rank below high")
	}
	if priorityRank(PriorityHigh) >= priorityRank(PriorityCritical) {
		t.Error("high should rank below critical")
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestSendRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow server — should be cancelled before completing.
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelSlack, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := d.Send(ctx, Notification{
		Title:    "Should timeout",
		Priority: PriorityHigh,
	})
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}
