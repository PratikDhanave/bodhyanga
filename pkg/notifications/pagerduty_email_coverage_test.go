package notifications

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendPagerDuty_ViaDispatcher(t *testing.T) {
	// The sendPagerDuty method hardcodes the PagerDuty URL, but we can
	// intercept it by using the dispatcher's custom http client.
	var received bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		w.WriteHeader(202)
	}))
	defer srv.Close()

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelPagerDuty, Enabled: true, Token: "test-routing-key", MinPriority: PriorityLow},
	})
	// Override the http client to point to our test server.
	// We can't directly change the PagerDuty endpoint, but we can make
	// sendPagerDuty call our server by replacing the client's transport.
	d.client = &http.Client{
		Transport: &rewriteTransport{
			base:    http.DefaultTransport,
			target:  srv.URL,
		},
	}

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "PD Test",
		Body:     "body",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !received {
		t.Fatal("PagerDuty endpoint was not called")
	}
}

// rewriteTransport rewrites request URLs to point to a local test server.
type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = t.target[len("http://"):]
	return t.base.RoundTrip(req)
}

func TestSendEmail_WithSMTPConfig(t *testing.T) {
	// Set up a fake SMTP server that just accepts connections.
	// We can't easily test the full SMTP flow, but we can test the
	// environment variable handling and error paths.

	// With all vars set but invalid host, it should try to connect and fail.
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "1") // unlikely to be running
	t.Setenv("SMTP_FROM", "bodhyanga@example.com")

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelEmail, Enabled: true, Endpoint: "to@example.com", Token: "smtp-password", MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Email Test",
		Body:     "body",
	})
	// Should fail because SMTP server isn't running.
	if err == nil {
		t.Fatal("expected error connecting to non-existent SMTP server")
	}
}

func TestSendEmail_DefaultPort(t *testing.T) {
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_FROM", "bodhyanga@example.com")

	d := NewDispatcher([]ChannelConfig{
		{Channel: ChannelEmail, Enabled: true, Endpoint: "to@example.com", MinPriority: PriorityLow},
	})

	err := d.Send(context.Background(), Notification{
		Priority: PriorityHigh,
		Title:    "Email Test",
		Body:     "body",
	})
	// Should fail because SMTP server isn't running on default port.
	if err == nil {
		t.Fatal("expected error")
	}
}
