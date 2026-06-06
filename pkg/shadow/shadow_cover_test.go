package shadow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNewConcordanceLedger_InvalidPath verifies that a bad file path
// returns an error.
func TestNewConcordanceLedger_InvalidPath(t *testing.T) {
	_, err := NewConcordanceLedger("/nonexistent-dir-abc123/ledger.jsonl")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

// TestConcordanceLedger_CloseNoFile verifies that Close on a memory-only
// ledger returns nil.
func TestConcordanceLedger_CloseNoFile(t *testing.T) {
	l, err := NewConcordanceLedger("")
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("Close on memory-only ledger: %v", err)
	}
}

// TestConcordanceLedger_AppendAfterClose exercises the write-error path:
// appending after the file has been closed should trigger the file.Write
// error branch (logged to stderr, not returned).
func TestConcordanceLedger_AppendAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	l, err := NewConcordanceLedger(path)
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}
	// Close the file first.
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Append after close: reopen the struct's file pointer to a closed fd
	// so file.Write will fail. This tests the error path in Append.
	closedFile, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	closedFile.Close()
	l.file = closedFile // closed file descriptor — Write will error
	l.Append(ConcordanceRecord{CaseID: "after-close"})

	// The record should still be in memory.
	report := l.Summarize()
	if report.Total != 1 {
		t.Errorf("Total = %d, want 1 (in-memory append should succeed)", report.Total)
	}
}

// TestNewShadowRunner_DefaultTimeout verifies that a zero timeout defaults
// to 10 seconds.
func TestNewShadowRunner_DefaultTimeout(t *testing.T) {
	l, _ := NewConcordanceLedger("")
	runner := NewShadowRunner(nil, nil, l, ModeSync, 0)
	if runner.timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want 10s", runner.timeout)
	}
}

// TestExtractCaseID_LongFallback verifies truncation of long chief complaints.
func TestExtractCaseID_LongFallback(t *testing.T) {
	long := "this is a very long chief complaint that exceeds thirty two characters easily"
	got := extractCaseID("{}", long)
	if len(got) > 32 {
		t.Errorf("expected truncated ID <= 32 chars, got %d: %q", len(got), got)
	}
}

// TestExtractCaseID_InvalidJSON verifies fallback when JSON is malformed.
func TestExtractCaseID_InvalidJSON(t *testing.T) {
	got := extractCaseID("not-json{{{", "fallback complaint")
	if got != "fallback-complaint" {
		t.Errorf("expected fallback ID, got %q", got)
	}
}

// TestRunShadow_InvalidJSON verifies that runAndRecord returns an error on
// unparseable case input.
func TestRunShadow_InvalidJSON(t *testing.T) {
	l, _ := NewConcordanceLedger("")
	runner := NewShadowRunner(nil, nil, l, ModeSync, time.Second)
	_, err := runner.runAndRecord(context.Background(), "NOT-JSON", "Pneumonia")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestRunShadow_ContextCancelled verifies that runAndRecord respects ctx
// cancellation.
func TestRunShadow_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	orch, bus, err := BuildShadowPipeline(context.Background(), "rule")
	if err != nil {
		t.Fatalf("BuildShadowPipeline: %v", err)
	}
	_ = orch

	l, _ := NewConcordanceLedger("")
	runner := NewShadowRunner(orch, bus, l, ModeSync, time.Second)

	caseInput := `{"chief_complaint": "cough", "hpi": "test", "demographics": "50M", "vitals": "normal", "case_id": "ctx-cancel"}`
	_, err = runner.RunShadow(ctx, caseInput, "test")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestBuildShadowPipeline_InvalidProvider verifies error for unknown provider.
func TestBuildShadowPipeline_InvalidProvider(t *testing.T) {
	_, _, err := BuildShadowPipeline(context.Background(), "nonexistent-provider-xyz")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

// TestConcordanceRecord_JSONRoundtrip verifies full JSON serialization.
func TestConcordanceRecord_JSONRoundtrip(t *testing.T) {
	rec := ConcordanceRecord{
		CaseID:            "rt-1",
		ClinicianDecision: "ACS",
		AIDecision:        "STEMI",
		Concordant:        false,
		AIConfidence:      0.75,
		Timestamp:         time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		LatencyMs:         300,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got ConcordanceRecord
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.CaseID != rec.CaseID {
		t.Errorf("CaseID mismatch: got %q, want %q", got.CaseID, rec.CaseID)
	}
	if got.Concordant != rec.Concordant {
		t.Errorf("Concordant mismatch: got %v, want %v", got.Concordant, rec.Concordant)
	}
}
