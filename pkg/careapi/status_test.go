package careapi

import "testing"

func TestStatus_ResolvesSubmittedCase(t *testing.T) {
	ta := newTestAPI(t, nil)
	var h handleResponse
	ta.doJSON(t, "POST", "/v1/cases", chestPain, &h)

	var e Entry
	code := ta.doJSON(t, "GET", "/v1/cases/"+h.CaseToken, nil, &e)
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if e.Status != StatusAwaitingReview || e.CaseToken != h.CaseToken {
		t.Errorf("entry = %+v", e)
	}
	// A parked case surfaces its diagnosis-under-review, no raw payload.
	if e.Diagnosis == "" {
		t.Error("awaiting_review entry should carry the diagnosis under review")
	}
}

func TestStatus_UnknownTokenIs404(t *testing.T) {
	ta := newTestAPI(t, nil)
	code, _ := ta.do(t, "GET", "/v1/cases/nope", nil)
	if code != 404 {
		t.Fatalf("status = %d, want 404", code)
	}
}
