package audit

import (
	"os"
	"testing"
)

// TestFileStore_EmptyPath tests OpenFile with empty path.
func TestFileStore_EmptyPath(t *testing.T) {
	_, err := OpenFile("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

// TestFileStore_BadPath tests OpenFile with invalid path.
func TestFileStore_BadPath(t *testing.T) {
	_, err := OpenFile("/nonexistent/dir/audit.log")
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}

// TestFileStore_CloseIdempotent tests double-close safety.
func TestFileStore_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	fs, err := OpenFile(dir + "/close.log")
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fs.Close(); err != nil {
		t.Fatalf("second close should be nil, got: %v", err)
	}
}

// TestFileStore_ListAfterSync tests that List returns events after Sync.
func TestFileStore_ListAfterSync(t *testing.T) {
	dir := t.TempDir()
	fs, err := OpenFile(dir + "/sync.log")
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()

	fs.Record(Event{Kind: KindMessage, AgentID: "test-sync"})
	events := fs.List()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].AgentID != "test-sync" {
		t.Fatalf("expected agent test-sync, got %s", events[0].AgentID)
	}
}

// TestFileStore_ListWithCorruptLine tests that corrupt lines are silently dropped.
func TestFileStore_ListWithCorruptLine(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/corrupt.log"

	// Write a valid line followed by a corrupt line.
	fs, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fs.Record(Event{Kind: KindMessage, AgentID: "valid"})
	fs.Close()

	// Manually append a corrupt line.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.Write([]byte("this is not json\n"))
	f.Close()

	// Reopen and list.
	fs2, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fs2.Close()

	events := fs2.List()
	if len(events) != 1 {
		t.Fatalf("expected 1 valid event (corrupt skipped), got %d", len(events))
	}
}

// TestFileStore_ListDeletedFile tests List when the file has been deleted.
func TestFileStore_ListDeletedFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/deleted.log"
	fs, _ := OpenFile(path)
	defer fs.Close()

	fs.Record(Event{Kind: KindMessage})

	// Delete the file.
	os.Remove(path)

	// List should return nil (cannot open file for read).
	events := fs.List()
	if events != nil {
		t.Fatalf("expected nil events after file deletion, got %d", len(events))
	}
}

// TestPickStrings_NilMap tests pickStrings with nil metadata.
func TestPickStrings_NilMap(t *testing.T) {
	var e Event
	pickStrings(nil, &e)
	if e.PatientID != "" || e.CaseID != "" || e.TraceID != "" || e.TenantID != "" {
		t.Fatal("expected empty fields from nil map")
	}
}

// TestPickStrings_NonStringValues tests pickStrings ignores non-string values.
func TestPickStrings_NonStringValues(t *testing.T) {
	var e Event
	m := map[string]any{
		"patient_id": 123,       // not a string
		"case_id":    true,      // not a string
		"trace_id":   []int{1},  // not a string
		"tenant_id":  "tenant1", // valid string
	}
	pickStrings(m, &e)
	if e.PatientID != "" {
		t.Fatalf("expected empty PatientID for int value, got %q", e.PatientID)
	}
	if e.TenantID != "tenant1" {
		t.Fatalf("expected tenant1, got %q", e.TenantID)
	}
}
