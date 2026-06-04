package phi

import (
	"regexp"
	"testing"
)

// TestAdd covers Redactor.Add (previously 0%).
func TestAdd(t *testing.T) {
	r := NewRedactor()
	origLen := len(r.Patterns)

	// Add a custom pattern for internal MRN format.
	r.Add(Pattern{
		Name:    "internal_mrn",
		Re:      regexp.MustCompile(`\bEPIC-\d{8}\b`),
		Replace: "[REDACTED-INTERNAL-MRN]",
	})

	if len(r.Patterns) != origLen+1 {
		t.Fatalf("expected %d patterns after Add, got %d", origLen+1, len(r.Patterns))
	}

	// Verify the added pattern works.
	input := "Patient EPIC-12345678 admitted"
	got := r.Redact(input)
	want := "Patient [REDACTED-INTERNAL-MRN] admitted"
	if got != want {
		t.Errorf("Redact after Add:\n got  %q\n want %q", got, want)
	}
}

// TestAdd_MultiplePatternsOrder ensures custom patterns run after defaults.
func TestAdd_MultiplePatternsOrder(t *testing.T) {
	r := NewRedactor()
	r.Add(Pattern{
		Name:    "custom1",
		Re:      regexp.MustCompile(`\bCUSTOM-\d+\b`),
		Replace: "[CUSTOM]",
	})
	r.Add(Pattern{
		Name:    "custom2",
		Re:      regexp.MustCompile(`\bINTERNAL-\d+\b`),
		Replace: "[INTERNAL]",
	})

	input := "See CUSTOM-123 and INTERNAL-456"
	got := r.Redact(input)
	want := "See [CUSTOM] and [INTERNAL]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestPrintf_NilRedactor covers the nil Redactor path (line 201).
func TestPrintf_NilRedactor(t *testing.T) {
	w := &LoggerWrapper{
		Redactor: nil,
		Inner:    &mockLogger{},
	}
	// Should not panic; should be a no-op.
	w.Printf("test %s", "hello")
}

// TestPrintf_NilInner covers the nil Inner path (line 201).
func TestPrintf_NilInner(t *testing.T) {
	w := &LoggerWrapper{
		Redactor: NewRedactor(),
		Inner:    nil,
	}
	// Should not panic; should be a no-op.
	w.Printf("test %s", "hello")
}

// TestPrintf_BothNil covers both nil case.
func TestPrintf_BothNil(t *testing.T) {
	w := &LoggerWrapper{}
	w.Printf("test %s", "hello")
}

// TestPrintf_NonStringArgs covers the non-string arg pass-through (line 212).
func TestPrintf_NonStringArgs(t *testing.T) {
	ml := &mockLogger{}
	w := &LoggerWrapper{
		Redactor: NewRedactor(),
		Inner:    ml,
	}
	// Pass non-string args (int, float64, bool). They should pass through.
	w.Printf("count=%d cost=%f ok=%v", 42, 3.14, true)

	if ml.lastFormat == "" {
		t.Fatal("Printf was not called on inner logger")
	}
	// Verify no redaction occurred on the format string (no PHI patterns).
	if ml.lastFormat != "count=%d cost=%f ok=%v" {
		t.Errorf("format was modified: got %q", ml.lastFormat)
	}
}

// TestPrintf_RedactsFormatString covers PHI in the format string itself (line 215).
func TestPrintf_RedactsFormatString(t *testing.T) {
	ml := &mockLogger{}
	w := &LoggerWrapper{
		Redactor: NewRedactor(),
		Inner:    ml,
	}
	// PHI in the format string (SSN pattern).
	w.Printf("treating MRN: 123-45-6789 patient %s", "test")

	if ml.lastFormat != "treating MRN: [REDACTED-SSN] patient %s" {
		t.Errorf("format string PHI not redacted: got %q", ml.lastFormat)
	}
}

// TestPrintf_RedactsStringArgs covers PHI in string args (line 209-210).
func TestPrintf_RedactsStringArgs(t *testing.T) {
	ml := &mockLogger{}
	w := &LoggerWrapper{
		Redactor: NewRedactor(),
		Inner:    ml,
	}
	w.Printf("patient info: %s", "SSN is 123-45-6789")

	if len(ml.lastArgs) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(ml.lastArgs))
	}
	got, ok := ml.lastArgs[0].(string)
	if !ok {
		t.Fatal("arg should be string")
	}
	if got != "SSN is [REDACTED-SSN]" {
		t.Errorf("string arg PHI not redacted: got %q", got)
	}
}

// TestPrintf_MixedArgs covers both string and non-string args together.
func TestPrintf_MixedArgs(t *testing.T) {
	ml := &mockLogger{}
	w := &LoggerWrapper{
		Redactor: NewRedactor(),
		Inner:    ml,
	}
	w.Printf("patient %s age %d phone %s", "MRN: ABC123", 65, "call 555.123.4567")

	if len(ml.lastArgs) != 3 {
		t.Fatalf("expected 3 args, got %d", len(ml.lastArgs))
	}
	// First arg is string with MRN pattern.
	s1, _ := ml.lastArgs[0].(string)
	if s1 != "[REDACTED-MRN]" {
		t.Errorf("arg[0] not redacted: got %q", s1)
	}
	// Second arg is int, passed through.
	if ml.lastArgs[1] != 65 {
		t.Errorf("arg[1] should be 65, got %v", ml.lastArgs[1])
	}
	// Third arg is string with phone pattern.
	s3, _ := ml.lastArgs[2].(string)
	if s3 != "call [REDACTED-PHONE]" {
		t.Errorf("arg[2] not redacted: got %q", s3)
	}
}

// mockLogger captures Printf calls for assertions.
type mockLogger struct {
	lastFormat string
	lastArgs   []any
}

func (m *mockLogger) Printf(format string, v ...any) {
	m.lastFormat = format
	m.lastArgs = v
}
