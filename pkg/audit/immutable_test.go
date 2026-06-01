package audit

import (
	"encoding/json"
	"os"
	"testing"
)

// makeTestEvent builds a minimal audit Event for testing.
func makeTestEvent(i int) Event {
	return Event{
		Kind:      KindMessage,
		AgentID:   "test-agent",
		MessageID: "msg-" + string(rune('a'+i)),
		CaseID:    "case-001",
	}
}

// TestImmutableRecordAndVerify records 10 events and verifies the chain is intact.
func TestImmutableRecordAndVerify(t *testing.T) {
	f, err := os.CreateTemp("", "audit-immutable-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	rec, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatalf("NewImmutableRecorder: %v", err)
	}

	const n = 10
	for i := 0; i < n; i++ {
		if err := rec.Record(makeTestEvent(i)); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ok, brokenAt, err := rec.VerifyChain(path)
	if err != nil {
		t.Fatalf("VerifyChain error: %v", err)
	}
	if !ok {
		t.Fatalf("chain broken at seq %d, expected intact chain", brokenAt)
	}
}

// TestImmutableTamperDetection tampers with one JSON line and expects the
// chain verification to fail at that sequence number.
func TestImmutableTamperDetection(t *testing.T) {
	f, err := os.CreateTemp("", "audit-tamper-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	rec, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatalf("NewImmutableRecorder: %v", err)
	}

	const n = 10
	for i := 0; i < n; i++ {
		if err := rec.Record(makeTestEvent(i)); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read all lines.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Split into lines.
	lines := splitLines(data)
	if len(lines) < n {
		t.Fatalf("expected %d lines, got %d", n, len(lines))
	}

	// Tamper line at index 4 (seq 4): change the AgentID in the event.
	const tamperIdx = 4
	var ce ChainedEvent
	if err := json.Unmarshal([]byte(lines[tamperIdx]), &ce); err != nil {
		t.Fatalf("unmarshal line %d: %v", tamperIdx, err)
	}
	ce.Event.AgentID = "evil-agent" // mutate the stored event
	tampered, err := json.Marshal(ce)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	lines[tamperIdx] = string(tampered)

	// Write tampered file.
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0o600); err != nil {
		t.Fatal(err)
	}

	ok, brokenAt, err := rec.VerifyChain(path)
	if err != nil {
		t.Fatalf("VerifyChain error: %v", err)
	}
	if ok {
		t.Fatal("expected chain to be broken, but VerifyChain returned ok=true")
	}
	if brokenAt != tamperIdx {
		t.Fatalf("expected chain break at seq %d, got %d", tamperIdx, brokenAt)
	}
}

// TestImmutableRestore verifies that closing and reopening the recorder
// continues the chain from where it left off.
func TestImmutableRestore(t *testing.T) {
	f, err := os.CreateTemp("", "audit-restore-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	// First session: record 5 events.
	rec1, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatalf("NewImmutableRecorder (first): %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := rec1.Record(makeTestEvent(i)); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}
	hashAfterFirst := rec1.prevHash
	if err := rec1.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	// Second session: reopen and record 5 more events.
	rec2, err := NewImmutableRecorder(path, nil, nil)
	if err != nil {
		t.Fatalf("NewImmutableRecorder (second): %v", err)
	}
	// The restored prevHash must match the last hash written by the first session.
	if rec2.prevHash != hashAfterFirst {
		t.Fatalf("restored prevHash mismatch:\n  want %s\n  got  %s", hashAfterFirst, rec2.prevHash)
	}
	if rec2.seq != 5 {
		t.Fatalf("restored seq = %d, want 5", rec2.seq)
	}
	for i := 5; i < 10; i++ {
		if err := rec2.Record(makeTestEvent(i)); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}
	if err := rec2.Close(); err != nil {
		t.Fatalf("Close second: %v", err)
	}

	// Full chain of 10 events should verify cleanly.
	ok, brokenAt, err := rec2.VerifyChain(path)
	if err != nil {
		t.Fatalf("VerifyChain error: %v", err)
	}
	if !ok {
		t.Fatalf("chain broken at seq %d after restore, expected intact", brokenAt)
	}
}

// splitLines splits JSONL bytes into a slice of non-empty line strings.
func splitLines(data []byte) []string {
	var out []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				out = append(out, string(data[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(data) {
		out = append(out, string(data[start:]))
	}
	return out
}

// joinLines joins line strings back to JSONL bytes.
func joinLines(lines []string) string {
	var out string
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
