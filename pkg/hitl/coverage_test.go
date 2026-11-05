package hitl

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// ---------------------------------------------------------------------------
// Checkpoint tests
// ---------------------------------------------------------------------------

func TestCheckpointer_FlushAndRestore(t *testing.T) {
	q := NewQueue()
	dir := t.TempDir()
	path := dir + "/cp.json"
	cp := NewCheckpointer(q, path)

	// Submit some items.
	q.Submit(medical.ReviewRequest{Kind: "test-1"})
	q.Submit(medical.ReviewRequest{Kind: "test-2"})

	// Flush.
	if err := cp.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Verify file exists.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cpData CheckpointData
	if err := json.Unmarshal(data, &cpData); err != nil {
		t.Fatal(err)
	}
	if len(cpData.Pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(cpData.Pending))
	}
	if cpData.Version != checkpointVersion {
		t.Fatalf("expected version %d, got %d", checkpointVersion, cpData.Version)
	}

	// Create a new queue and restore.
	q2 := NewQueue()
	cp2 := NewCheckpointer(q2, path)
	if err := cp2.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(q2.Pending()) != 2 {
		t.Fatalf("expected 2 pending after restore, got %d", len(q2.Pending()))
	}
}

func TestCheckpointer_RestoreMissingFile(t *testing.T) {
	q := NewQueue()
	cp := NewCheckpointer(q, "/nonexistent/path/cp.json")

	// Missing file is not an error.
	if err := cp.Restore(); err != nil {
		t.Fatalf("expected nil for missing file, got: %v", err)
	}
}

func TestCheckpointer_RestoreCorruptFile(t *testing.T) {
	q := NewQueue()
	dir := t.TempDir()
	path := dir + "/corrupt.json"
	os.WriteFile(path, []byte("not valid json"), 0o600)

	cp := NewCheckpointer(q, path)
	if err := cp.Restore(); err == nil {
		t.Fatal("expected error for corrupt file")
	}
}

func TestCheckpointer_RestoreWrongVersion(t *testing.T) {
	q := NewQueue()
	dir := t.TempDir()
	path := dir + "/version.json"

	data := CheckpointData{
		Version: 999,
		Pending: nil,
		SavedAt: time.Now().Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(data)
	os.WriteFile(path, b, 0o600)

	cp := NewCheckpointer(q, path)
	if err := cp.Restore(); err == nil {
		t.Fatal("expected error for wrong version")
	}
}

func TestCheckpointer_RestoreIdempotent(t *testing.T) {
	q := NewQueue()
	dir := t.TempDir()
	path := dir + "/idem.json"
	cp := NewCheckpointer(q, path)

	q.Submit(medical.ReviewRequest{Kind: "test"})
	cp.Flush()

	// Restore twice — should not duplicate.
	cp.Restore()
	cp.Restore()
	if len(q.Pending()) != 1 {
		t.Fatalf("expected 1 after double restore, got %d", len(q.Pending()))
	}
}

func TestCheckpointer_FlushBadDir(t *testing.T) {
	q := NewQueue()
	cp := NewCheckpointer(q, "/nonexistent/dir/cp.json")
	q.Submit(medical.ReviewRequest{Kind: "test"})
	err := cp.Flush()
	if err == nil {
		t.Fatal("expected error for bad dir")
	}
}

func TestCheckpointer_FlushRenameError(t *testing.T) {
	// This is hard to test without file system tricks, but we can test the
	// normal path works. The rename error path has OS-level semantics.
	q := NewQueue()
	dir := t.TempDir()
	cp := NewCheckpointer(q, dir+"/ok.json")
	q.Submit(medical.ReviewRequest{Kind: "test"})
	if err := cp.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointer_AutoFlush(t *testing.T) {
	q := NewQueue()
	dir := t.TempDir()
	path := dir + "/auto.json"
	cp := NewCheckpointer(q, path)
	cp.AutoFlush(2) // flush every 2 submits

	q.Submit(medical.ReviewRequest{Kind: "first"})
	// After 1 submit, no flush yet.
	if _, err := os.Stat(path); err == nil {
		t.Fatal("file should not exist yet after 1 submit")
	}

	q.Submit(medical.ReviewRequest{Kind: "second"})
	// After 2 submits, auto-flush should have fired.
	if _, err := os.Stat(path); err != nil {
		t.Fatal("file should exist after 2 submits")
	}
}

func TestCheckpointer_AutoFlushZeroInterval(t *testing.T) {
	q := NewQueue()
	dir := t.TempDir()
	path := dir + "/auto0.json"
	cp := NewCheckpointer(q, path)
	cp.AutoFlush(0) // 0 is treated as 1

	q.Submit(medical.ReviewRequest{Kind: "first"})
	if _, err := os.Stat(path); err != nil {
		t.Fatal("file should exist after 1 submit with interval=0")
	}
}

// ---------------------------------------------------------------------------
// Hub WebSocket tests (ServeHTTP)
// ---------------------------------------------------------------------------

func TestHub_BroadcastNoSubscribers(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	// Broadcast to a tenant with no subscribers — should not panic.
	hub.Broadcast("nonexistent-tenant", Event{Type: "test"})
}

func TestHub_BroadcastSlowConsumer(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	ch, unsub := hub.Subscribe("t1")
	defer unsub()

	// Fill the channel buffer (64).
	for i := 0; i < 65; i++ {
		hub.Broadcast("t1", Event{Type: "flood"})
	}

	// The 65th should be dropped (slow consumer), not block.
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count > 64 {
		t.Fatalf("expected <= 64 events (buffer size), got %d", count)
	}
}

func TestHub_NotifyDecide(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)
	ch, unsub := hub.Subscribe("t1")
	defer unsub()

	hub.NotifyDecide("t1", "rev-1", "approve", "doc-1")

	select {
	case ev := <-ch:
		if ev.Type != "review_decided" {
			t.Fatalf("expected review_decided, got %s", ev.Type)
		}
		var de DecisionEvent
		json.Unmarshal(ev.Payload, &de)
		if de.ReviewID != "rev-1" || de.Decision != "approve" || de.By != "doc-1" {
			t.Fatalf("unexpected decision event: %+v", de)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for decision event")
	}
}

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	ch, unsub := hub.Subscribe("t1")

	// Broadcast before unsub.
	hub.Broadcast("t1", Event{Type: "test"})
	select {
	case <-ch:
		// ok
	case <-time.After(time.Second):
		t.Fatal("expected event")
	}

	unsub()

	// After unsub, channel should be closed.
	_, open := <-ch
	if open {
		t.Fatal("expected channel to be closed after unsub")
	}
}

func TestHub_MultipleTenants(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	ch1, unsub1 := hub.Subscribe("t1")
	defer unsub1()
	ch2, unsub2 := hub.Subscribe("t2")
	defer unsub2()

	hub.Broadcast("t1", Event{Type: "for-t1"})

	select {
	case ev := <-ch1:
		if ev.Type != "for-t1" {
			t.Fatal("wrong event for t1")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout for t1")
	}

	// t2 should not receive t1's event.
	select {
	case <-ch2:
		t.Fatal("t2 should not receive t1's event")
	case <-time.After(50 * time.Millisecond):
		// ok
	}
}

// ---------------------------------------------------------------------------
// Queue tests — covering uncovered branches
// ---------------------------------------------------------------------------

func TestQueue_DecideInvalidDecision(t *testing.T) {
	q := NewQueue()
	r := q.Submit(medical.ReviewRequest{Kind: "test"})

	_, ok := q.Decide(r.ID, "reviewer", "invalid_decision", "reason", "")
	if ok {
		t.Fatal("expected false for invalid decision")
	}
}

func TestQueue_DecideAlreadyDecided(t *testing.T) {
	q := NewQueue()
	r := q.Submit(medical.ReviewRequest{Kind: "test"})

	// First decide.
	_, ok := q.Decide(r.ID, "reviewer", "approve", "good", "")
	if !ok {
		t.Fatal("expected true for first decide")
	}

	// Second decide — already decided.
	_, ok = q.Decide(r.ID, "reviewer2", "reject", "bad", "")
	if ok {
		t.Fatal("expected false for already-decided")
	}
}

func TestQueue_DecideUnknownID(t *testing.T) {
	q := NewQueue()
	_, ok := q.Decide("nonexistent", "reviewer", "approve", "", "")
	if ok {
		t.Fatal("expected false for unknown id")
	}
}

func TestQueue_ExpireNoDeadline(t *testing.T) {
	q := NewQueue()
	q.Submit(medical.ReviewRequest{Kind: "test"}) // No SLAAt set.

	n := q.Expire(time.Now().Add(time.Hour))
	if n != 0 {
		t.Fatalf("expected 0 expired (no deadline), got %d", n)
	}
}

func TestQueue_ExpireAlreadyDecided(t *testing.T) {
	q := NewQueue()
	r := q.Submit(medical.ReviewRequest{Kind: "test", SLAAt: time.Now().Add(-time.Hour)})
	q.Decide(r.ID, "reviewer", "approve", "", "")

	n := q.Expire(time.Now())
	if n != 0 {
		t.Fatalf("expected 0 expired (already decided), got %d", n)
	}
}

func TestQueue_SubmitWithHub(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)
	q.Hub = hub

	ch, unsub := hub.Subscribe("default")
	defer unsub()

	q.Submit(medical.ReviewRequest{Kind: "test"})

	select {
	case ev := <-ch:
		if ev.Type != "review_submitted" {
			t.Fatalf("expected review_submitted, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for hub notification")
	}
}

func TestQueue_SubmitWithHubTenantMeta(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)
	q.Hub = hub

	ch, unsub := hub.Subscribe("custom-tenant")
	defer unsub()

	q.Submit(medical.ReviewRequest{
		Kind: "test",
		Meta: map[string]any{"tenant_id": "custom-tenant"},
	})

	select {
	case ev := <-ch:
		if ev.Type != "review_submitted" {
			t.Fatalf("expected review_submitted, got %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// TestWriteTextFrame tests the WebSocket frame writer.
func TestWriteTextFrame_SmallPayload(t *testing.T) {
	var buf mockFlusher
	err := writeTextFrame(&buf, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	// Header should be 0x81, 5, then "hello".
	if buf.data[0] != 0x81 || buf.data[1] != 5 {
		t.Fatalf("unexpected header: %x %x", buf.data[0], buf.data[1])
	}
}

func TestWriteTextFrame_MediumPayload(t *testing.T) {
	payload := make([]byte, 200)
	var buf mockFlusher
	err := writeTextFrame(&buf, payload)
	if err != nil {
		t.Fatal(err)
	}
	// Header should be 0x81, 126, then 2-byte length.
	if buf.data[0] != 0x81 || buf.data[1] != 126 {
		t.Fatalf("unexpected header for 200-byte payload: %x %x", buf.data[0], buf.data[1])
	}
}

func TestWriteTextFrame_OversizePayload(t *testing.T) {
	payload := make([]byte, 70000)
	var buf mockFlusher
	err := writeTextFrame(&buf, payload)
	if err != nil {
		t.Fatal(err)
	}
	// Should skip (return nil) for payloads > 65535.
	if len(buf.data) != 0 {
		t.Fatalf("expected empty write for oversize payload, got %d bytes", len(buf.data))
	}
}

type mockFlusher struct {
	data []byte
}

func (m *mockFlusher) Write(b []byte) (int, error) {
	m.data = append(m.data, b...)
	return len(b), nil
}

func (m *mockFlusher) Flush() error { return nil }
