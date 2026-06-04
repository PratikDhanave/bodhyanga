package notifications

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPriorityRank_Default(t *testing.T) {
	// Unknown priority maps to 0.
	if priorityRank("bogus") != 0 {
		t.Fatal("expected 0 for unknown priority")
	}
}

func TestPriorityRank_AllLevels(t *testing.T) {
	tests := []struct {
		p    Priority
		want int
	}{
		{PriorityLow, 0},
		{PriorityMedium, 1},
		{PriorityHigh, 2},
		{PriorityCritical, 3},
	}
	for _, tt := range tests {
		if got := priorityRank(tt.p); got != tt.want {
			t.Fatalf("priorityRank(%s) = %d, want %d", tt.p, got, tt.want)
		}
	}
}

func TestSendSlack_Coverage(t *testing.T) {
	var received bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelSlack, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Test",
		Body:     "Test body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("Slack endpoint was not called")
	}
}

func TestSendTeams_Coverage(t *testing.T) {
	var received bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelTeams, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Test",
		Body:     "Teams body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("Teams endpoint was not called")
	}
}

func TestSendPagerDuty_SeverityMapping(t *testing.T) {
	// Test all PagerDuty severity mappings.
	for _, p := range []Priority{PriorityLow, PriorityMedium, PriorityHigh, PriorityCritical} {
		sev := mapPagerDutySeverity(p)
		if sev == "" {
			t.Fatalf("empty severity for %s", p)
		}
	}
	if mapPagerDutySeverity("bogus") != "info" {
		t.Fatal("expected info for unknown priority")
	}
}

func TestSendWebhook_Coverage(t *testing.T) {
	var received bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("expected Content-Type application/json")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelWebhook, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Webhook Test",
		Body:     "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !received {
		t.Fatal("webhook was not called")
	}
}

func TestSendEmail_NoSMTPHost(t *testing.T) {
	t.Setenv("SMTP_HOST", "")
	t.Setenv("SMTP_FROM", "test@example.com")

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelEmail, Enabled: true, Endpoint: "to@example.com", MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Email Test",
		Body:     "body",
	})
	if err == nil {
		t.Fatal("expected error when SMTP_HOST not set")
	}
	if !strings.Contains(err.Error(), "SMTP_HOST") {
		t.Fatalf("expected SMTP_HOST error, got: %v", err)
	}
}

func TestSendEmail_NoSMTPFrom(t *testing.T) {
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_FROM", "")

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelEmail, Enabled: true, Endpoint: "to@example.com", MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Email Test",
		Body:     "body",
	})
	if err == nil {
		t.Fatal("expected error when SMTP_FROM not set")
	}
	if !strings.Contains(err.Error(), "SMTP_FROM") {
		t.Fatalf("expected SMTP_FROM error, got: %v", err)
	}
}

func TestSend_DisabledChannel(t *testing.T) {
	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelSlack, Enabled: false, Endpoint: "http://localhost", MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Test",
	})
	if err != nil {
		t.Fatal("expected nil for disabled channel")
	}
}

func TestSend_PriorityFiltering(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelWebhook, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityHigh},
	})

	// Send low priority — should be filtered out.
	err := d.Send(context.Background(), Notification{
		Priority: PriorityLow,
		Title:    "Low priority",
	})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("low priority should be filtered out")
	}
}

func TestSend_UnknownChannel(t *testing.T) {
	d := NewDispatcher([]ChannelConfig{
		{Channel: "carrier_pigeon", Enabled: true, MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Test",
	})
	if err == nil {
		t.Fatal("expected error for unknown channel")
	}
	if !strings.Contains(err.Error(), "carrier_pigeon") {
		t.Fatalf("expected channel name in error, got: %v", err)
	}
}

func TestSend_Non2xxResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelWebhook, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Test",
	})
	if err == nil {
		t.Fatal("expected error for 503")
	}
}

func TestRecordSent_Overflow(t *testing.T) {
	d := NewDispatcher(nil)

	for i := 0; i < maxHistory+50; i++ {
		d.recordSent(Notification{Title: "n"})
	}

	if len(d.sent) > maxHistory {
		t.Fatalf("expected <= %d, got %d", maxHistory, len(d.sent))
	}
}

func TestRecentNotifications_Coverage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelWebhook, Enabled: true, Endpoint: srv.URL, MinPriority: PriorityLow},
	})

	for i := 0; i < 5; i++ {
		d.Send(context.Background(), Notification{
			Priority: PriorityHigh,
			Title:    "Test",
		})
	}

	recent := d.RecentNotifications(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3, got %d", len(recent))
	}

	// Get all.
	all := d.RecentNotifications(0)
	if len(all) != 5 {
		t.Fatalf("expected 5, got %d", len(all))
	}

	// Limit exceeds buffer.
	more := d.RecentNotifications(100)
	if len(more) != 5 {
		t.Fatalf("expected 5, got %d", len(more))
	}
}

func TestSend_MultipleChannels_PartialFailure(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer okSrv.Close()

	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer failSrv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelWebhook, Enabled: true, Endpoint: failSrv.URL, MinPriority: PriorityLow},
		{Channel: ChannelSlack, Enabled: true, Endpoint: okSrv.URL, MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Test",
	})
	// First error should be returned, but both channels were attempted.
	if err == nil {
		t.Fatal("expected error from failing channel")
	}
}
