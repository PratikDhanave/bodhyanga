package hitl

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// ---------------------------------------------------------------------------
// Queue.Decide — approve_with_modifications branch
// ---------------------------------------------------------------------------

func TestQueue_DecideApproveWithModifications(t *testing.T) {
	q := NewQueue()
	r := q.Submit(medical.ReviewRequest{
		Kind:    "care_plan_review",
		Subject: "test modifications",
	})

	updated, ok := q.Decide(r.ID, "dr-smith", "approve_with_modifications", "dosage adjusted", "increase to 500mg")
	if !ok {
		t.Fatal("expected Decide to succeed for approve_with_modifications")
	}
	if updated.Status != medical.ReviewModified {
		t.Fatalf("expected status %q, got %q", medical.ReviewModified, updated.Status)
	}
	if updated.ReviewerID != "dr-smith" {
		t.Fatalf("expected reviewer dr-smith, got %q", updated.ReviewerID)
	}
	if updated.Reason != "dosage adjusted" {
		t.Fatalf("expected reason 'dosage adjusted', got %q", updated.Reason)
	}
	if updated.Modification != "increase to 500mg" {
		t.Fatalf("expected modification 'increase to 500mg', got %q", updated.Modification)
	}
	if updated.DecidedAt.IsZero() {
		t.Fatal("expected non-zero DecidedAt")
	}

	// Verify idempotency: second decide on same ID should return false.
	_, ok = q.Decide(r.ID, "dr-jones", "reject", "changed mind", "")
	if ok {
		t.Fatal("expected false for re-deciding an already-decided review")
	}
}

func TestQueue_DecideReject(t *testing.T) {
	q := NewQueue()
	r := q.Submit(medical.ReviewRequest{
		Kind:    "diagnosis_review",
		Subject: "test reject",
	})

	updated, ok := q.Decide(r.ID, "dr-jones", "reject", "insufficient evidence", "")
	if !ok {
		t.Fatal("expected Decide to succeed for reject")
	}
	if updated.Status != medical.ReviewRejected {
		t.Fatalf("expected status %q, got %q", medical.ReviewRejected, updated.Status)
	}
	if updated.Reason != "insufficient evidence" {
		t.Fatalf("expected reason 'insufficient evidence', got %q", updated.Reason)
	}
}

// ---------------------------------------------------------------------------
// writeTextFrame — write error paths
// ---------------------------------------------------------------------------

// errFlusher simulates write failures.
type errFlusher struct {
	failOn    int // which Write call (1-based) should fail
	writeCall int
	flushErr  error
}

func (e *errFlusher) Write(b []byte) (int, error) {
	e.writeCall++
	if e.writeCall == e.failOn {
		return 0, errors.New("write error")
	}
	return len(b), nil
}

func (e *errFlusher) Flush() error {
	return e.flushErr
}

func TestWriteTextFrame_HeaderWriteError(t *testing.T) {
	f := &errFlusher{failOn: 1} // fail on header write
	err := writeTextFrame(f, []byte("hello"))
	if err == nil {
		t.Fatal("expected error from header write failure")
	}
}

func TestWriteTextFrame_PayloadWriteError(t *testing.T) {
	f := &errFlusher{failOn: 2} // fail on payload write
	err := writeTextFrame(f, []byte("hello"))
	if err == nil {
		t.Fatal("expected error from payload write failure")
	}
}

func TestWriteTextFrame_FlushError(t *testing.T) {
	f := &errFlusher{flushErr: errors.New("flush error")}
	err := writeTextFrame(f, []byte("hello"))
	if err == nil {
		t.Fatal("expected error from flush failure")
	}
}

func TestWriteTextFrame_EmptyPayload(t *testing.T) {
	var buf mockFlusher
	err := writeTextFrame(&buf, []byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Header should be 0x81, 0 (FIN+TEXT, length 0).
	if len(buf.data) < 2 {
		t.Fatal("expected at least 2 bytes header")
	}
	if buf.data[0] != 0x81 || buf.data[1] != 0 {
		t.Fatalf("unexpected header: 0x%02x 0x%02x", buf.data[0], buf.data[1])
	}
}

// ---------------------------------------------------------------------------
// Checkpoint.Flush — rename error (write to read-only dir)
// ---------------------------------------------------------------------------

func TestCheckpointer_FlushRenameFail(t *testing.T) {
	q := NewQueue()
	q.Submit(medical.ReviewRequest{Kind: "test"})
	// Use an invalid path that will cause a write failure.
	cp := NewCheckpointer(q, "/dev/null/impossible/cp.json")
	err := cp.Flush()
	if err == nil {
		t.Fatal("expected error from Flush to invalid path")
	}
}

func TestCheckpointer_FlushRenameFailCrossDevice(t *testing.T) {
	q := NewQueue()
	q.Submit(medical.ReviewRequest{Kind: "test"})

	// Write the .tmp file to a valid dir, but try to rename to an impossible
	// destination. The trick: set path to "<valid_dir>/sub/cp.json" where
	// "sub" doesn't exist. The tmp file writes to "<valid_dir>/sub/cp.json.tmp"
	// which will also fail on write. Instead, we test by making the target
	// path a directory (rename file -> directory is invalid on all OSes).
	dir := t.TempDir()
	target := dir + "/target"
	// Create target as a directory so rename from .tmp file to directory fails.
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	// But we need the .tmp write to succeed, which means target+".tmp" path
	// must be writable. target is a dir, so target+".tmp" = dir+"/target.tmp"
	// which is a valid file path in dir.
	cp := NewCheckpointer(q, target)
	err := cp.Flush()
	if err == nil {
		t.Fatal("expected error from Flush when rename target is a directory")
	}
	// Verify .tmp cleanup: target.tmp should be removed.
	if _, statErr := os.Stat(target + ".tmp"); !os.IsNotExist(statErr) {
		t.Error("expected .tmp file to be cleaned up after rename failure")
	}
}

// ---------------------------------------------------------------------------
// Checkpoint.Restore — non-NotExist read error
// ---------------------------------------------------------------------------

func TestCheckpointer_RestoreUnreadableFile(t *testing.T) {
	q := NewQueue()
	// A directory path instead of a file causes a different error than NotExist.
	dir := t.TempDir()
	cp := NewCheckpointer(q, dir)
	err := cp.Restore()
	// On macOS/Linux, reading a directory as a file returns a non-nil error
	// that is NOT os.IsNotExist. This exercises the read-error branch.
	if err == nil {
		t.Fatal("expected error from reading a directory as checkpoint file")
	}
}

// ---------------------------------------------------------------------------
// Queue — Submit preserves existing ID and SubmittedAt
// ---------------------------------------------------------------------------

func TestQueue_SubmitPreservesExistingIDAndTime(t *testing.T) {
	q := NewQueue()
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	r := q.Submit(medical.ReviewRequest{
		ID:          "custom-id",
		Kind:        "test",
		SubmittedAt: now,
	})
	if r.ID != "custom-id" {
		t.Fatalf("expected custom ID preserved, got %q", r.ID)
	}
	if !r.SubmittedAt.Equal(now) {
		t.Fatalf("expected SubmittedAt preserved, got %v", r.SubmittedAt)
	}
}

// ---------------------------------------------------------------------------
// Queue — CountByKind and CountByStatus
// ---------------------------------------------------------------------------

func TestQueue_CountByKindAndStatus(t *testing.T) {
	q := NewQueue()
	q.Submit(medical.ReviewRequest{Kind: "diag"})
	q.Submit(medical.ReviewRequest{Kind: "diag"})
	r := q.Submit(medical.ReviewRequest{Kind: "care"})
	q.Decide(r.ID, "rev", "approve", "", "")

	byKind := q.CountByKind()
	if byKind["diag"] != 2 {
		t.Errorf("expected 2 diag, got %d", byKind["diag"])
	}
	if byKind["care"] != 1 {
		t.Errorf("expected 1 care, got %d", byKind["care"])
	}

	byStatus := q.CountByStatus()
	if byStatus[medical.ReviewPending] != 2 {
		t.Errorf("expected 2 pending, got %d", byStatus[medical.ReviewPending])
	}
	if byStatus[medical.ReviewApproved] != 1 {
		t.Errorf("expected 1 approved, got %d", byStatus[medical.ReviewApproved])
	}
}

// ---------------------------------------------------------------------------
// Queue.Expire — multiple items, mixed states
// ---------------------------------------------------------------------------

func TestQueue_ExpireMixed(t *testing.T) {
	q := NewQueue()
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	// Expired SLA.
	q.Submit(medical.ReviewRequest{Kind: "urgent", SLAAt: past})
	// Future SLA.
	q.Submit(medical.ReviewRequest{Kind: "normal", SLAAt: future})
	// No SLA.
	q.Submit(medical.ReviewRequest{Kind: "indefinite"})

	expired := q.Expire(time.Now())
	if expired != 1 {
		t.Fatalf("expected 1 expired, got %d", expired)
	}

	byStatus := q.CountByStatus()
	if byStatus[medical.ReviewExpired] != 1 {
		t.Errorf("expected 1 expired in status count, got %d", byStatus[medical.ReviewExpired])
	}
	if byStatus[medical.ReviewPending] != 2 {
		t.Errorf("expected 2 pending in status count, got %d", byStatus[medical.ReviewPending])
	}
}

// ---------------------------------------------------------------------------
// Hub — Broadcast to non-existent tenant
// ---------------------------------------------------------------------------

func TestHub_BroadcastEmptyTenant(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)
	// Should not panic.
	hub.Broadcast("", Event{Type: "test"})
}

// ---------------------------------------------------------------------------
// Hub — Subscribe and unsubscribe last subscriber cleans up tenant map
// ---------------------------------------------------------------------------

func TestHub_UnsubscribeLastCleansMap(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	_, unsub := hub.Subscribe("cleanup-test")
	unsub()

	hub.mu.RLock()
	_, exists := hub.clients["cleanup-test"]
	hub.mu.RUnlock()

	if exists {
		t.Fatal("expected tenant entry to be removed after last unsubscribe")
	}
}

// ---------------------------------------------------------------------------
// writeTextFrame — medium payload with extended length
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// ServeHTTP — WebSocket connection receives event then client disconnects
// ---------------------------------------------------------------------------

func TestServeHTTP_ClientDisconnectAfterEvent(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	srv := httptest.NewServer(hub)
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	// WebSocket upgrade handshake.
	reqStr := "GET /ws/hitl?tenant_id=disconnect-test HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"
	fmt.Fprint(conn, reqStr)

	rd := bufio.NewReader(conn)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	statusLine, err := rd.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101, got: %s", statusLine)
	}
	// Consume remaining headers.
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	// Send an event; read the frame.
	hub.Broadcast("disconnect-test", Event{Type: "pre-disconnect", At: time.Now().Format(time.RFC3339Nano)})
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4096)
	n, err := rd.Read(buf)
	if err != nil || n < 2 {
		t.Fatalf("failed to read frame: n=%d err=%v", n, err)
	}

	// Close the connection; server should detect and clean up.
	conn.Close()
	time.Sleep(100 * time.Millisecond)
}

func TestWriteTextFrame_MediumPayloadExtended(t *testing.T) {
	// Payload of exactly 126 bytes to test the extended length encoding.
	payload := make([]byte, 126)
	for i := range payload {
		payload[i] = 'x'
	}
	var buf mockFlusher
	err := writeTextFrame(&buf, payload)
	if err != nil {
		t.Fatal(err)
	}
	// Header: 0x81, 126, then 2-byte big-endian length.
	if buf.data[0] != 0x81 || buf.data[1] != 126 {
		t.Fatalf("unexpected header for 126-byte payload: 0x%02x 0x%02x", buf.data[0], buf.data[1])
	}
	// Extended length bytes: 0x00, 0x7E (126 in big-endian).
	if buf.data[2] != 0x00 || buf.data[3] != 0x7E {
		t.Fatalf("unexpected extended length: 0x%02x 0x%02x", buf.data[2], buf.data[3])
	}
}
