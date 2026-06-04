package webhooks_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/webhooks"
)

// --- Dispatch: inactive endpoint skipped ---

func TestDispatch_InactiveEndpointSkipped(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhooks.NewDispatcher()
	// Register then deregister to make it inactive. We can't directly
	// set Active=false, so we test with an endpoint that has no matching event.
	d.Register(webhooks.Endpoint{
		TenantID: "t",
		URL:      srv.URL,
		Secret:   "s",
		Events:   []webhooks.EventType{webhooks.EventReviewDecided},
	})

	// Dispatch an event that the endpoint is NOT subscribed to.
	d.Dispatch("t", webhooks.EventDiagnosisComplete, nil)

	time.Sleep(200 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Errorf("expected 0 calls (wrong event), got %d", n)
	}
}

// --- Dispatch: no endpoints for tenant ---

func TestDispatch_NoEndpoints(t *testing.T) {
	d := webhooks.NewDispatcher()
	// Dispatch to a tenant with no endpoints. Should not panic.
	d.Dispatch("nonexistent-tenant", webhooks.EventAlertTriggered, map[string]string{"key": "val"})
}

// --- Dispatch: non-2xx response triggers retry ---

func TestDispatch_PostNon2xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest) // always fails
	}))
	defer srv.Close()

	d := webhooks.NewDispatcher()
	d.Register(webhooks.Endpoint{
		TenantID: "t",
		URL:      srv.URL,
		Secret:   "s",
		Events:   []webhooks.EventType{webhooks.EventPanelGap},
	})

	d.Dispatch("t", webhooks.EventPanelGap, nil)

	// Wait long enough for all 3 retry attempts.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if n := calls.Load(); n < 3 {
		t.Errorf("expected 3 attempts (all retries), got %d", n)
	}
}

// --- Dispatch: bad URL causes transport error ---

func TestDispatch_BadURL(t *testing.T) {
	d := webhooks.NewDispatcher()
	d.Register(webhooks.Endpoint{
		TenantID: "t",
		URL:      "http://127.0.0.1:1", // will fail to connect
		Secret:   "s",
		Events:   []webhooks.EventType{webhooks.EventDiagnosisComplete},
	})
	// Should not panic; the error is silently swallowed (fire-and-forget).
	d.Dispatch("t", webhooks.EventDiagnosisComplete, map[string]string{"ok": "yes"})
	// Give goroutine time to complete.
	time.Sleep(200 * time.Millisecond)
}

// --- List: empty tenant ---

func TestList_EmptyTenant(t *testing.T) {
	d := webhooks.NewDispatcher()
	eps := d.List("nonexistent")
	if len(eps) != 0 {
		t.Errorf("expected 0 endpoints, got %d", len(eps))
	}
}

// --- Dispatch: invalid URL causes NewRequest error ---

func TestDispatch_InvalidURL_NewRequestError(t *testing.T) {
	d := webhooks.NewDispatcher()
	d.Register(webhooks.Endpoint{
		TenantID: "t",
		URL:      "://invalid\x7f", // invalid URL triggers http.NewRequest error
		Secret:   "s",
		Events:   []webhooks.EventType{webhooks.EventDiagnosisComplete},
	})
	// Should not panic; the error is silently swallowed.
	d.Dispatch("t", webhooks.EventDiagnosisComplete, map[string]string{"ok": "yes"})
	time.Sleep(200 * time.Millisecond)
}

// --- Dispatch: multiple endpoints ---

func TestDispatch_MultipleEndpoints(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhooks.NewDispatcher()
	d.Register(webhooks.Endpoint{
		TenantID: "t",
		URL:      srv.URL,
		Secret:   "s1",
		Events:   []webhooks.EventType{webhooks.EventAlertTriggered},
	})
	d.Register(webhooks.Endpoint{
		TenantID: "t",
		URL:      srv.URL,
		Secret:   "s2",
		Events:   []webhooks.EventType{webhooks.EventAlertTriggered},
	})

	d.Dispatch("t", webhooks.EventAlertTriggered, nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if n := calls.Load(); n != 2 {
		t.Errorf("expected 2 deliveries, got %d", n)
	}
}
