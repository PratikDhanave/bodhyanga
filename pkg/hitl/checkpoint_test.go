package hitl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// tempPath returns a path inside a temporary directory that is cleaned up
// when the test ends. The file itself is not created.
func tempPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "hitl-queue.json")
}

// TestCheckpointFlushRestore verifies the basic round-trip: items submitted
// to a queue can be flushed to disk and then restored into a fresh queue.
func TestCheckpointFlushRestore(t *testing.T) {
	path := tempPath(t)

	// --- phase 1: populate and flush ---
	q1 := NewQueue()
	cp1 := NewCheckpointer(q1, path)

	r1 := q1.Submit(medical.ReviewRequest{
		Kind:      "diagnosis_review",
		PatientID: "p1",
		NextTo:    "diagnostic_supervisor",
		NextType:  "diagnosis",
		Payload:   `{"finding":"pneumonia"}`,
		Priority:  "high",
	})
	r2 := q1.Submit(medical.ReviewRequest{
		Kind:      "care_plan_review",
		PatientID: "p2",
		NextTo:    "engagement",
		NextType:  "care_plan_ready",
		Payload:   `{"plan":"inhaler"}`,
		Priority:  "normal",
	})

	// Decide one before flushing: it must NOT appear in the restore.
	if _, ok := q1.Decide(r2.ID, "md-1", "approve", "looks good", ""); !ok {
		t.Fatal("Decide failed")
	}

	if err := cp1.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Checkpoint file must exist after Flush.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("checkpoint file missing after Flush: %v", err)
	}

	// --- phase 2: restore into a fresh queue ---
	q2 := NewQueue()
	cp2 := NewCheckpointer(q2, path)

	if err := cp2.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	pending := q2.Pending()
	if len(pending) != 1 {
		t.Fatalf("want 1 pending after restore, got %d", len(pending))
	}
	if pending[0].ID != r1.ID {
		t.Errorf("restored wrong ID: want %s, got %s", r1.ID, pending[0].ID)
	}
	if pending[0].Kind != "diagnosis_review" {
		t.Errorf("restored wrong Kind: %s", pending[0].Kind)
	}
	if pending[0].Payload != `{"finding":"pneumonia"}` {
		t.Errorf("restored wrong Payload: %s", pending[0].Payload)
	}
	if pending[0].Status != medical.ReviewPending {
		t.Errorf("restored item should be pending, got %s", pending[0].Status)
	}

	// r2 was approved before flush; it must NOT appear in the restored queue.
	if _, ok := q2.Get(r2.ID); ok {
		t.Errorf("approved item should not be restored")
	}
}

// TestCheckpointRestoreMissingFile verifies that Restore returns nil (not an
// error) when the checkpoint file does not exist.
func TestCheckpointRestoreMissingFile(t *testing.T) {
	q := NewQueue()
	cp := NewCheckpointer(q, filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err := cp.Restore(); err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
}

// TestCheckpointIdempotentRestore verifies that calling Restore twice does
// not duplicate items in the queue.
func TestCheckpointIdempotentRestore(t *testing.T) {
	path := tempPath(t)

	q1 := NewQueue()
	q1.Submit(medical.ReviewRequest{
		Kind: "k1", NextTo: "x", NextType: "t", Payload: "{}",
	})
	cp1 := NewCheckpointer(q1, path)
	if err := cp1.Flush(); err != nil {
		t.Fatal(err)
	}

	q2 := NewQueue()
	cp2 := NewCheckpointer(q2, path)
	if err := cp2.Restore(); err != nil {
		t.Fatal(err)
	}
	if err := cp2.Restore(); err != nil {
		t.Fatal(err)
	}
	if n := len(q2.Pending()); n != 1 {
		t.Errorf("want 1 pending after double restore, got %d", n)
	}
}

// TestCheckpointEmptyQueue verifies that Flush + Restore with an empty queue
// does not error and results in an empty queue.
func TestCheckpointEmptyQueue(t *testing.T) {
	path := tempPath(t)

	q := NewQueue()
	cp := NewCheckpointer(q, path)
	if err := cp.Flush(); err != nil {
		t.Fatalf("Flush empty queue: %v", err)
	}

	q2 := NewQueue()
	cp2 := NewCheckpointer(q2, path)
	if err := cp2.Restore(); err != nil {
		t.Fatalf("Restore empty checkpoint: %v", err)
	}
	if n := len(q2.Pending()); n != 0 {
		t.Errorf("want 0 pending, got %d", n)
	}
}

// TestCheckpointFileContents verifies that the on-disk JSON contains the
// expected version field and SavedAt timestamp.
func TestCheckpointFileContents(t *testing.T) {
	path := tempPath(t)

	q := NewQueue()
	q.Submit(medical.ReviewRequest{
		Kind: "k", NextTo: "x", NextType: "t", Payload: "{}",
	})
	cp := NewCheckpointer(q, path)
	if err := cp.Flush(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data CheckpointData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("checkpoint JSON malformed: %v", err)
	}
	if data.Version != checkpointVersion {
		t.Errorf("want version %d, got %d", checkpointVersion, data.Version)
	}
	if data.SavedAt == "" {
		t.Error("SavedAt should not be empty")
	}
	if len(data.Pending) != 1 {
		t.Errorf("want 1 pending in file, got %d", len(data.Pending))
	}
}

// TestCheckpointAutoFlush verifies that AutoFlush triggers Flush every N
// Submit calls.
func TestCheckpointAutoFlush(t *testing.T) {
	path := tempPath(t)

	q := NewQueue()
	cp := NewCheckpointer(q, path)
	cp.AutoFlush(3)

	// 2 submits — no flush yet (checkpoint file should not exist).
	q.Submit(medical.ReviewRequest{Kind: "k", NextTo: "x", NextType: "t", Payload: "{}"})
	q.Submit(medical.ReviewRequest{Kind: "k", NextTo: "x", NextType: "t", Payload: "{}"})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no checkpoint before 3rd submit, but stat returned: %v", err)
	}

	// 3rd submit — flush must have been triggered.
	q.Submit(medical.ReviewRequest{Kind: "k", NextTo: "x", NextType: "t", Payload: "{}"})
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected checkpoint after 3rd submit, stat error: %v", err)
	}

	// Verify content: 3 pending items.
	raw, _ := os.ReadFile(path)
	var data CheckpointData
	_ = json.Unmarshal(raw, &data)
	if len(data.Pending) != 3 {
		t.Errorf("want 3 pending in file after auto-flush, got %d", len(data.Pending))
	}
}

// TestCheckpointAtomicWrite verifies that Flush does not leave a .tmp file on
// success (the rename succeeded and only the final path exists).
func TestCheckpointAtomicWrite(t *testing.T) {
	path := tempPath(t)

	q := NewQueue()
	cp := NewCheckpointer(q, path)
	if err := cp.Flush(); err != nil {
		t.Fatal(err)
	}

	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("tmp file should be gone after successful Flush, stat: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("final checkpoint file missing: %v", err)
	}
}
