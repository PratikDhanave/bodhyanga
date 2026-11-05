package careapi

import (
	"testing"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
)

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

// specs/006: per-case cost reaches the HTTP surface. A "chest pain" case orders
// troponin ($45) + ecg ($30) = $75, which must appear on the status Entry and
// the review summary.
func TestStatus_SurfacesPerCaseCost(t *testing.T) {
	ta := newTestAPI(t, nil)
	var h handleResponse
	ta.doJSON(t, "POST", "/v1/cases",
		agents.PatientCase{CaseID: "cost-1", Symptoms: []string{"chest pain"}}, &h)

	var e Entry
	ta.doJSON(t, "GET", "/v1/cases/"+h.CaseToken, nil, &e)
	if e.CostUSD != 75 {
		t.Errorf("status cost = $%.0f, want $75 (troponin+ecg)", e.CostUSD)
	}

	var body reviewsBody
	ta.doJSON(t, "GET", "/v1/reviews", nil, &body)
	if len(body.Reviews) != 1 || body.Reviews[0].CostUSD != 75 {
		t.Errorf("review summary cost = %+v, want $75", body.Reviews)
	}
}

func TestStatus_UnknownTokenIs404(t *testing.T) {
	ta := newTestAPI(t, nil)
	code, _ := ta.do(t, "GET", "/v1/cases/nope", nil)
	if code != 404 {
		t.Fatalf("status = %d, want 404", code)
	}
}
