package careapi

import (
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
)

type auditBody struct {
	CaseToken string       `json:"case_token"`
	Events    []auditEvent `json:"events"`
}

func TestAudit_CompletedCaseTrailIsOrderedAndPHIFree(t *testing.T) {
	ta := newTestAPI(t, nil)
	h := submitPaused(t, ta)
	var e Entry
	ta.doJSON(t, "POST", "/v1/reviews/"+h.SessionID, answerRequest{Approved: true, ReviewerID: "dr-x"}, &e)

	var body auditBody
	code := ta.doJSON(t, "GET", "/v1/cases/"+h.CaseToken+"/audit", nil, &body)
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(body.Events) == 0 {
		t.Fatal("empty trail for a completed case")
	}
	// Every event is for this case token, and admission precedes execution.
	sawAdmission := false
	for _, ev := range body.Events {
		if ev.CaseToken != h.CaseToken {
			t.Errorf("trail carries a foreign token: %+v", ev)
		}
		if ev.Kind == "policy_decision" {
			sawAdmission = true
		}
	}
	if !sawAdmission {
		t.Error("no admission record in the trail")
	}
	// PHI scan.
	blob, _ := marshal(body)
	for _, phi := range []string{"PT-SECRET-42", "chest pain", "hypertension"} {
		if strings.Contains(blob, phi) {
			t.Errorf("audit trail leaked %q", phi)
		}
	}
}

func TestAudit_UnknownTokenIs404(t *testing.T) {
	ta := newTestAPI(t, nil)
	code, _ := ta.do(t, "GET", "/v1/cases/ghost/audit", nil)
	if code != 404 {
		t.Fatalf("status = %d, want 404", code)
	}
}

// A denied case still exposes its denial record on the trail.
func TestAudit_DeniedCaseShowsDenial(t *testing.T) {
	ta := newTestAPI(t, admit.DenyAll())
	var h handleResponse
	ta.doJSON(t, "POST", "/v1/cases", chestPain, &h)

	var body auditBody
	code := ta.doJSON(t, "GET", "/v1/cases/"+h.CaseToken+"/audit", nil, &body)
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	sawDeny := false
	for _, ev := range body.Events {
		if ev.Decision == "deny" {
			sawDeny = true
		}
	}
	if !sawDeny {
		t.Error("denied case trail has no deny record")
	}
}
