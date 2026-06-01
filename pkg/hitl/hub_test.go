package hitl

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// TestHubSubscribeBroadcast verifies that a subscribed channel receives
// events broadcast for its tenant and does NOT receive events for a
// different tenant.
func TestHubSubscribeBroadcast(t *testing.T) {
	q := NewQueue()
	h := NewHub(q)

	ch, unsub := h.Subscribe("tenant-a")
	defer unsub()

	ev := Event{Type: "review_submitted", At: time.Now().UTC().Format(time.RFC3339Nano)}
	h.Broadcast("tenant-a", ev)

	select {
	case got := <-ch:
		if got.Type != "review_submitted" {
			t.Fatalf("expected type review_submitted, got %q", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event on correct tenant channel")
	}

	// Broadcast to a different tenant — should NOT appear on ch.
	h.Broadcast("tenant-b", Event{Type: "review_submitted", At: ev.At})
	select {
	case unexpected := <-ch:
		t.Fatalf("received unexpected cross-tenant event: %+v", unexpected)
	case <-time.After(50 * time.Millisecond):
		// Correct — nothing received.
	}
}

// TestHubUnsubscribe verifies that after calling unsub no further events
// are delivered and the channel is closed.
func TestHubUnsubscribe(t *testing.T) {
	q := NewQueue()
	h := NewHub(q)

	ch, unsub := h.Subscribe("tenant-x")
	unsub()

	// Channel should be closed.
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected channel to be closed after unsub, but it is still open")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel to close")
	}

	// Broadcast after unsub must not panic.
	h.Broadcast("tenant-x", Event{Type: "ping", At: time.Now().UTC().Format(time.RFC3339Nano)})
}

// TestHubMultipleSubscribers verifies that all subscribers for a tenant
// receive the same broadcast event.
func TestHubMultipleSubscribers(t *testing.T) {
	q := NewQueue()
	h := NewHub(q)

	ch1, unsub1 := h.Subscribe("tenant-m")
	defer unsub1()
	ch2, unsub2 := h.Subscribe("tenant-m")
	defer unsub2()

	ev := Event{Type: "review_decided", At: time.Now().UTC().Format(time.RFC3339Nano)}
	h.Broadcast("tenant-m", ev)

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Type != "review_decided" {
				t.Fatalf("subscriber %d: expected review_decided, got %q", i+1, got.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i+1)
		}
	}
}

// TestHubNotifySubmit verifies that NotifySubmit produces a correctly shaped
// "review_submitted" event with the ReviewRequest encoded in Payload.
func TestHubNotifySubmit(t *testing.T) {
	q := NewQueue()
	h := NewHub(q)

	ch, unsub := h.Subscribe("tenant-s")
	defer unsub()

	rr := medical.ReviewRequest{
		ID:      "rr-001",
		Kind:    "diagnosis_review",
		Subject: "suspected ACS",
	}
	h.NotifySubmit("tenant-s", rr)

	select {
	case got := <-ch:
		if got.Type != "review_submitted" {
			t.Fatalf("expected review_submitted, got %q", got.Type)
		}
		var decoded medical.ReviewRequest
		if err := json.Unmarshal(got.Payload, &decoded); err != nil {
			t.Fatalf("payload unmarshal: %v", err)
		}
		if decoded.ID != "rr-001" {
			t.Fatalf("expected ID rr-001, got %q", decoded.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for NotifySubmit event")
	}
}

// TestHubNotifyDecide verifies that NotifyDecide produces a correctly shaped
// "review_decided" event with the DecisionEvent encoded in Payload.
func TestHubNotifyDecide(t *testing.T) {
	q := NewQueue()
	h := NewHub(q)

	ch, unsub := h.Subscribe("tenant-d")
	defer unsub()

	h.NotifyDecide("tenant-d", "rr-002", "approve", "dr-jones")

	select {
	case got := <-ch:
		if got.Type != "review_decided" {
			t.Fatalf("expected review_decided, got %q", got.Type)
		}
		var de DecisionEvent
		if err := json.Unmarshal(got.Payload, &de); err != nil {
			t.Fatalf("payload unmarshal: %v", err)
		}
		if de.ReviewID != "rr-002" {
			t.Fatalf("expected ReviewID rr-002, got %q", de.ReviewID)
		}
		if de.Decision != "approve" {
			t.Fatalf("expected Decision approve, got %q", de.Decision)
		}
		if de.By != "dr-jones" {
			t.Fatalf("expected By dr-jones, got %q", de.By)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for NotifyDecide event")
	}
}

// TestQueueSubmitNotifiesHub verifies the end-to-end path: Queue.Submit with
// a Hub set triggers hub.NotifySubmit, which delivers the event to
// subscribers.
func TestQueueSubmitNotifiesHub(t *testing.T) {
	q := NewQueue()
	h := NewHub(q)
	q.Hub = h

	ch, unsub := h.Subscribe("default")
	defer unsub()

	q.Submit(medical.ReviewRequest{
		Kind:    "care_plan_review",
		Subject: "DM care plan",
	})

	select {
	case got := <-ch:
		if got.Type != "review_submitted" {
			t.Fatalf("expected review_submitted from Queue.Submit, got %q", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out — Queue.Submit did not notify hub")
	}
}

// TestQueueSubmitWithTenantIDInMeta verifies that the tenant_id envelope key
// in ReviewRequest.Meta is used to scope the broadcast correctly.
func TestQueueSubmitWithTenantIDInMeta(t *testing.T) {
	q := NewQueue()
	h := NewHub(q)
	q.Hub = h

	chCorrect, unsubCorrect := h.Subscribe("clinic-42")
	defer unsubCorrect()
	chOther, unsubOther := h.Subscribe("default")
	defer unsubOther()

	q.Submit(medical.ReviewRequest{
		Kind:    "refill_review",
		Subject: "metformin refill",
		Meta:    map[string]any{"tenant_id": "clinic-42"},
	})

	select {
	case got := <-chCorrect:
		if got.Type != "review_submitted" {
			t.Fatalf("expected review_submitted on correct tenant, got %q", got.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out — event not delivered to correct tenant")
	}

	// Ensure no cross-tenant delivery.
	select {
	case unexpected := <-chOther:
		t.Fatalf("cross-tenant delivery to 'default': %+v", unexpected)
	case <-time.After(50 * time.Millisecond):
		// Correct.
	}
}
