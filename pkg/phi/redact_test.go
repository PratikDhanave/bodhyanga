package phi

import (
	"fmt"
	"strings"
	"testing"
)

func TestRedactorDefaultPatterns(t *testing.T) {
	r := NewRedactor()
	cases := []struct {
		in, want string
	}{
		{"SSN 123-45-6789 done", "SSN [REDACTED-SSN] done"},
		{"patient MRN: ABC123 admitted", "patient [REDACTED-MRN] admitted"},
		{"call 415-555-0199 about results", "call [REDACTED-PHONE] about results"},
		{"reach j.doe@example.com today", "reach [REDACTED-EMAIL] today"},
		{"born 1960-01-15 confirmed", "born [REDACTED-DATE] confirmed"},
		{"appt 03/15/2026 ok", "appt [REDACTED-DATE] ok"},
		{"no PHI here", "no PHI here"},
	}
	for _, c := range cases {
		got := r.Redact(c.in)
		if got != c.want {
			t.Errorf("Redact(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

type captureLogger struct{ lines []string }

func (c *captureLogger) Printf(format string, v ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, v...))
}

func TestLoggerWrapperRedactsArgsAndFormat(t *testing.T) {
	inner := &captureLogger{}
	w := &LoggerWrapper{Redactor: NewRedactor(), Inner: inner}
	w.Printf("patient ssn=%s mrn=%s", "123-45-6789", "MRN: A1234567")
	if len(inner.lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(inner.lines))
	}
	line := inner.lines[0]
	if strings.Contains(line, "123-45-6789") || strings.Contains(line, "A1234567") {
		t.Errorf("PHI leaked: %q", line)
	}
	if !strings.Contains(line, "[REDACTED-SSN]") || !strings.Contains(line, "[REDACTED-MRN]") {
		t.Errorf("missing redaction sentinels: %q", line)
	}
}
