package admit

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"
)

// safeMarker is a test error that opts into verbatim audit text.
type safeMarker string

func (e safeMarker) Error() string { return string(e) }
func (safeMarker) AuditSafe()      {}

func TestSafeErrorText(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil yields empty", nil, ""},
		{"unsafe error is redacted to its type", errors.New("patient chest pain onset 3pm"), "redacted error (*errors.errorString)"},
		{"audit-safe passes through", safeMarker("inbound rejected: malformed"), "inbound rejected: malformed"},
		{"unsafe outer wrapper cannot smuggle payload; inner safe text wins", fmt.Errorf("leaky %w", safeMarker("cat")), "cat"},
	}
	for _, tt := range tests {
		if got := SafeErrorText(tt.err); got != tt.want {
			t.Errorf("%s: SafeErrorText = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestSafeErrorTextNeverLeaksUnsafeMessage(t *testing.T) {
	got := SafeErrorText(errors.New("SECRET symptom: hemoptysis"))
	if strings.Contains(got, "SECRET") || strings.Contains(got, "hemoptysis") {
		t.Fatalf("redaction leaked the message: %q", got)
	}
}

// RecordAgentError must route through the redaction guard: an unmarked error
// reaches the trail as a type descriptor, never its payload-bearing message.
func TestRecordAgentErrorRedactsUnsafeError(t *testing.T) {
	rec := &capturingRecorder{}
	b := New(AllowAll(), rec, nil)

	b.RecordAgentError("diagnostician", "abcd1234", errors.New("symptom fever in patient 42"))

	events := rec.List()
	if len(events) != 1 {
		t.Fatalf("got %d audit events, want 1", len(events))
	}
	if events[0].Kind != audit.KindAgentError {
		t.Fatalf("kind = %q, want %q", events[0].Kind, audit.KindAgentError)
	}
	if strings.Contains(events[0].ErrorText, "symptom") || strings.Contains(events[0].ErrorText, "patient") {
		t.Fatalf("audit ErrorText leaked payload: %q", events[0].ErrorText)
	}
}
