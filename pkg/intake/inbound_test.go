package intake

import (
	"strings"
	"testing"
)

// RejectError.Error() must be built only from the reason category (+ optional
// fingerprint) — never interpolated payload content (spec FR-005/FR-006).
func TestRejectError_MessageIsCategoryOnly(t *testing.T) {
	e := &RejectError{Reason: ReasonUnsupportedType}
	if got := e.Error(); !strings.Contains(got, string(ReasonUnsupportedType)) {
		t.Errorf("Error() = %q, want it to name the reason", got)
	}

	// With a fingerprint, the token appears but still no content.
	e2 := &RejectError{Reason: ReasonMissingSegment, Fingerprint: "deadbeef"}
	got := e2.Error()
	if !strings.Contains(got, "deadbeef") || !strings.Contains(got, string(ReasonMissingSegment)) {
		t.Errorf("Error() = %q, want reason + fingerprint", got)
	}
	// A fingerprint is an opaque hash, never a raw identifier.
	if strings.ContainsAny(got, " \n\t") && strings.Contains(got, "MRN") {
		t.Errorf("Error() looks like it carries content: %q", got)
	}
}
