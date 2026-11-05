package webhooks_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/webhooks"
)

// computeSig mirrors the sign() logic in webhooks.go so tests can verify
// the header without importing an unexported helper.
func computeSig(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// TestRegisterListDeregister verifies basic CRUD operations on the
// Dispatcher's endpoint store.
func TestRegisterListDeregister(t *testing.T) {
	d := webhooks.NewDispatcher()

	id := d.Register(webhooks.Endpoint{
		TenantID: "tenant-a",
		URL:      "https://example.com/hook",
		Secret:   "s3cr3t",
		Events:   []webhooks.EventType{webhooks.EventDiagnosisComplete},
	})

	if id == "" {
		t.Fatal("Register returned empty ID")
	}

	eps := d.List("tenant-a")
	if len(eps) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(eps))
	}
	if eps[0].ID != id {
		t.Errorf("listed ID %q != registered ID %q", eps[0].ID, id)
	}
	if !eps[0].Active {
		t.Error("newly registered endpoint should be Active")
	}

	d.Deregister("tenant-a", id)
	eps = d.List("tenant-a")
	if len(eps) != 0 {
		t.Fatalf("expected 0 endpoints after deregister, got %d", len(eps))
	}
}

// TestRegisterPreservesProvidedID checks that if the caller sets ep.ID the
// Dispatcher respects it.
func TestRegisterPreservesProvidedID(t *testing.T) {
	d := webhooks.NewDispatcher()
	id := d.Register(webhooks.Endpoint{
		ID:       "my-custom-id",
		TenantID: "t",
		URL:      "https://example.com/",
		Events:   []webhooks.EventType{webhooks.EventAlertTriggered},
	})
	if id != "my-custom-id" {
		t.Errorf("expected returned ID %q, got %q", "my-custom-id", id)
	}
}

// TestDispatchDeliveryAndSignature registers an httptest server, fires a
// Dispatch, and asserts that:
//   - exactly one POST was received
//   - the X-Bodhyanga-Signature header is correct
//   - the payload decodes to the expected event type
func TestDispatchDeliveryAndSignature(t *testing.T) {
	secret := "webhook-secret"

	var (
		mu       sync.Mutex
		received []struct {
			sig  string
			body []byte
		}
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf []byte
		buf = make([]byte, 0, 512)
		tmp := make([]byte, 512)
		for {
			n, err := r.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		mu.Lock()
		received = append(received, struct {
			sig  string
			body []byte
		}{r.Header.Get("X-Bodhyanga-Signature"), buf})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhooks.NewDispatcher()
	d.Register(webhooks.Endpoint{
		TenantID: "tenant1",
		URL:      srv.URL,
		Secret:   secret,
		Events:   []webhooks.EventType{webhooks.EventDiagnosisComplete},
	})

	d.Dispatch("tenant1", webhooks.EventDiagnosisComplete, map[string]string{
		"case_id":   "case-42",
		"diagnosis": "pneumonia",
	})

	// Dispatch is fire-and-forget; wait up to 2 s for delivery.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(received))
	}

	entry := received[0]

	// Verify signature header format.
	if !strings.HasPrefix(entry.sig, "sha256=") {
		t.Errorf("signature header %q missing sha256= prefix", entry.sig)
	}
	wantSig := "sha256=" + computeSig(secret, entry.body)
	if entry.sig != wantSig {
		t.Errorf("signature mismatch: got %q, want %q", entry.sig, wantSig)
	}

	// Verify payload shape.
	var p webhooks.WebhookPayload
	if err := json.Unmarshal(entry.body, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Event != webhooks.EventDiagnosisComplete {
		t.Errorf("event mismatch: got %q, want %q", p.Event, webhooks.EventDiagnosisComplete)
	}
	if p.TenantID != "tenant1" {
		t.Errorf("tenant_id mismatch: got %q, want %q", p.TenantID, "tenant1")
	}
	if p.ID == "" {
		t.Error("payload ID must not be empty")
	}
	if p.Timestamp == "" {
		t.Error("payload Timestamp must not be empty")
	}
}

// TestDispatchEventFiltering ensures that endpoints only receive events they
// subscribed to.
func TestDispatchEventFiltering(t *testing.T) {
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
		Secret:   "x",
		// Only interested in review events, NOT panel gaps.
		Events: []webhooks.EventType{webhooks.EventReviewSubmitted, webhooks.EventReviewDecided},
	})

	d.Dispatch("t", webhooks.EventPanelGap, map[string]string{"gap": "a1c"})

	// Wait briefly; the endpoint should NOT be called.
	time.Sleep(200 * time.Millisecond)

	if n := calls.Load(); n != 0 {
		t.Errorf("expected 0 deliveries for non-subscribed event, got %d", n)
	}

	// Now dispatch a subscribed event.
	d.Dispatch("t", webhooks.EventReviewSubmitted, map[string]string{"review_id": "r1"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if n := calls.Load(); n != 1 {
		t.Errorf("expected 1 delivery for subscribed event, got %d", n)
	}
}

// TestDispatchTenantIsolation ensures endpoints for one tenant don't receive
// events dispatched for another tenant.
func TestDispatchTenantIsolation(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := webhooks.NewDispatcher()
	d.Register(webhooks.Endpoint{
		TenantID: "tenant-A",
		URL:      srv.URL,
		Secret:   "x",
		Events:   []webhooks.EventType{webhooks.EventAlertTriggered},
	})

	// Dispatch for a different tenant.
	d.Dispatch("tenant-B", webhooks.EventAlertTriggered, nil)

	time.Sleep(200 * time.Millisecond)

	if n := calls.Load(); n != 0 {
		t.Errorf("expected 0 deliveries across tenant boundary, got %d", n)
	}
}

// TestDispatchRetryOnFailure registers a server that fails the first two
// requests then succeeds, and verifies that the Dispatcher retries until
// success.
func TestDispatchRetryOnFailure(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use a custom Dispatcher with a very short client timeout so the test
	// stays fast. We cannot override back-off sleeps without an internal hook,
	// so we accept that this test takes ~6 s in the worst case but use a
	// 10-second deadline.
	d := webhooks.NewDispatcher()
	d.Register(webhooks.Endpoint{
		TenantID: "t",
		URL:      srv.URL,
		Secret:   "s",
		Events:   []webhooks.EventType{webhooks.EventPanelGap},
	})

	d.Dispatch("t", webhooks.EventPanelGap, nil)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if n := calls.Load(); n < 3 {
		t.Errorf("expected at least 3 attempts (retry), got %d", n)
	}
}

// TestDeregisterUnknownIDIsNoop ensures Deregister does not panic on a missing
// ID.
func TestDeregisterUnknownIDIsNoop(t *testing.T) {
	d := webhooks.NewDispatcher()
	d.Register(webhooks.Endpoint{
		TenantID: "t",
		URL:      "https://example.com/",
		Events:   []webhooks.EventType{webhooks.EventDiagnosisComplete},
	})
	// Should not panic.
	d.Deregister("t", "does-not-exist")
	if n := len(d.List("t")); n != 1 {
		t.Errorf("expected 1 endpoint remaining, got %d", n)
	}
}
