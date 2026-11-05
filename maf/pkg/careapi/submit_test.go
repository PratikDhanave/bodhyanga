package careapi

import (
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
)

var chestPain = agents.PatientCase{
	CaseID: "PT-SECRET-42", Symptoms: []string{"crushing chest pain"}, History: []string{"hypertension"},
}

// A valid case returns a 201 handle whose token is the Runner's hashed token,
// and (Review on) parks at awaiting_review. The body leaks no raw id/symptom.
func TestSubmit_ValidCaseReturnsHandle(t *testing.T) {
	ta := newTestAPI(t, nil)
	var h handleResponse
	code := ta.doJSON(t, "POST", "/v1/cases", chestPain, &h)
	if code != 201 {
		t.Fatalf("status = %d, want 201", code)
	}
	if h.SessionID == "" {
		t.Error("empty session_id")
	}
	if h.CaseToken != agents.HashCaseID(chestPain.CaseID) {
		t.Errorf("case_token = %q, want the hashed token", h.CaseToken)
	}
	if h.Status != StatusAwaitingReview {
		t.Errorf("status = %q, want awaiting_review", h.Status)
	}
}

func TestSubmit_MalformedBodyIs400AndDoesNotRun(t *testing.T) {
	ta := newTestAPI(t, nil)
	code, _ := ta.do(t, "POST", "/v1/cases", "{ not json")
	if code != 400 {
		t.Fatalf("status = %d, want 400", code)
	}
	if len(ta.rec.List()) != 0 {
		t.Error("pipeline ran on a malformed body")
	}
}

// A governance denial is a normal 201 with status "denied", not a 500 — the
// boundary did its job (FR-011).
func TestSubmit_DenyAllIsGovernedNot500(t *testing.T) {
	ta := newTestAPI(t, admit.DenyAll())
	var h handleResponse
	code := ta.doJSON(t, "POST", "/v1/cases", chestPain, &h)
	if code != 201 {
		t.Fatalf("status = %d, want 201 (governed denial)", code)
	}
	if h.Status != StatusDenied {
		t.Errorf("status = %q, want denied", h.Status)
	}
}

// The submit response must never carry the raw case id or a symptom string.
func TestSubmit_ResponseIsPHIFree(t *testing.T) {
	ta := newTestAPI(t, nil)
	_, body := ta.do(t, "POST", "/v1/cases", chestPain)
	for _, phi := range []string{"PT-SECRET-42", "crushing chest pain", "hypertension"} {
		if strings.Contains(string(body), phi) {
			t.Errorf("response leaked %q: %s", phi, body)
		}
	}
}
