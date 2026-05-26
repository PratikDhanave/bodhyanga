package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// fixedEvent returns an audit.Event with stable At so the test output is
// reproducible regardless of clock skew.
func fixedEvent(kind audit.Kind, caseID, agentID, decision, reason string, at time.Time) audit.Event {
	return audit.Event{
		ID:       "evt-" + agentID + "-" + decision,
		At:       at,
		Kind:     kind,
		AgentID:  agentID,
		CaseID:   caseID,
		Decision: decision,
		Reason:   reason,
	}
}

func TestExplainHandler_UnknownCase(t *testing.T) {
	rec := audit.NewInMemory()
	store := eval.NewInMemoryStore()
	q := hitl.NewQueue()

	req := httptest.NewRequest(http.MethodGet, "/explain/case-unknown", nil)
	req.SetPathValue("case_id", "case-unknown")
	w := httptest.NewRecorder()

	explainHandler("tenant-test", rec, store, q)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var exp Explanation
	if err := json.Unmarshal(w.Body.Bytes(), &exp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if exp.CaseID != "case-unknown" {
		t.Errorf("CaseID = %q, want case-unknown", exp.CaseID)
	}
	if exp.Status != "unknown" {
		t.Errorf("Status = %q, want unknown", exp.Status)
	}
	if exp.TenantID != "tenant-test" {
		t.Errorf("TenantID = %q, want tenant-test", exp.TenantID)
	}
	if len(exp.AgentHops) != 0 {
		t.Errorf("AgentHops len = %d, want 0", len(exp.AgentHops))
	}
	if len(exp.Caveats) < 3 {
		t.Errorf("Caveats len = %d, want >= 3", len(exp.Caveats))
	}
}

func TestExplainHandler_RebuildsAgentHops(t *testing.T) {
	rec := audit.NewInMemory()
	store := eval.NewInMemoryStore()
	q := hitl.NewQueue()

	caseID := "case-001"
	base := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	_ = rec.Record(fixedEvent(audit.KindPolicyDecision, caseID, "orchestrator", "allow", "case_id present", base))
	_ = rec.Record(fixedEvent(audit.KindMessage, caseID, "intake", "", "", base.Add(1*time.Second)))
	_ = rec.Record(fixedEvent(audit.KindMessage, caseID, "questioner", "", "", base.Add(2*time.Second)))
	_ = rec.Record(fixedEvent(audit.KindMessage, caseID, "diagnostician", "", "", base.Add(3*time.Second)))

	req := httptest.NewRequest(http.MethodGet, "/explain/"+caseID, nil)
	req.SetPathValue("case_id", caseID)
	w := httptest.NewRecorder()

	explainHandler("tenant-test", rec, store, q)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var exp Explanation
	if err := json.Unmarshal(w.Body.Bytes(), &exp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if exp.Status != "pending" {
		t.Errorf("Status = %q, want pending (no eval record yet)", exp.Status)
	}
	if got, want := len(exp.AgentHops), 3; got != want {
		t.Errorf("AgentHops len = %d, want %d", got, want)
	}
	// Chronological order matters for an explanation.
	for i := 1; i < len(exp.AgentHops); i++ {
		if exp.AgentHops[i-1].At.After(exp.AgentHops[i].At) {
			t.Errorf("AgentHops out of chronological order at index %d", i)
		}
	}
	if exp.AgentHops[0].AgentID != "intake" {
		t.Errorf("first hop agent = %q, want intake", exp.AgentHops[0].AgentID)
	}
	if got, want := len(exp.PolicyDecisions), 1; got != want {
		t.Errorf("PolicyDecisions len = %d, want %d", got, want)
	}
	if exp.EventCounts["message"] != 3 {
		t.Errorf("EventCounts[message] = %d, want 3", exp.EventCounts["message"])
	}
}

func TestExplainHandler_CompletedCaseFromEvalStore(t *testing.T) {
	rec := audit.NewInMemory()
	store := eval.NewInMemoryStore()
	q := hitl.NewQueue()

	caseID := "case-cap-001"
	base := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	_ = rec.Record(fixedEvent(audit.KindMessage, caseID, "diagnostic_supervisor", "", "", base))

	// Eval record for a successful CAP diagnosis.
	underBudget := true
	_ = store.Save(eval.InteractionRecord{
		ID:       caseID,
		Scenario: caseID,
		Success:  true,
		Metrics: map[string]float64{
			"accuracy": 1.0,
			"cost_usd": 220.0,
		},
		Metadata: map[string]any{
			"case_id":         caseID,
			"diagnosis_match": true,
			"under_budget":    underBudget,
		},
		StartedAt: base,
		EndedAt:   base.Add(30 * time.Second),
	})

	req := httptest.NewRequest(http.MethodGet, "/explain/"+caseID, nil)
	req.SetPathValue("case_id", caseID)
	w := httptest.NewRecorder()

	explainHandler("tenant-test", rec, store, q)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var exp Explanation
	if err := json.Unmarshal(w.Body.Bytes(), &exp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if exp.Status != "completed" {
		t.Errorf("Status = %q, want completed", exp.Status)
	}
	if exp.Accuracy != 1.0 {
		t.Errorf("Accuracy = %v, want 1.0", exp.Accuracy)
	}
	if exp.TotalCostUSD != 220.0 {
		t.Errorf("TotalCostUSD = %v, want 220.0", exp.TotalCostUSD)
	}
	if exp.UnderBudget == nil || *exp.UnderBudget != true {
		t.Errorf("UnderBudget = %v, want pointer to true", exp.UnderBudget)
	}
}

func TestExplainHandler_FailedCase(t *testing.T) {
	rec := audit.NewInMemory()
	store := eval.NewInMemoryStore()
	q := hitl.NewQueue()

	caseID := "case-fail-001"
	base := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	_ = rec.Record(fixedEvent(audit.KindMessage, caseID, "diagnostician", "", "", base))
	errEvt := audit.Event{
		ID:        "evt-err",
		At:        base.Add(1 * time.Second),
		Kind:      audit.KindAgentError,
		AgentID:   "diagnostician",
		CaseID:    caseID,
		ErrorText: "context deadline exceeded",
	}
	_ = rec.Record(errEvt)

	req := httptest.NewRequest(http.MethodGet, "/explain/"+caseID, nil)
	req.SetPathValue("case_id", caseID)
	w := httptest.NewRecorder()

	explainHandler("tenant-test", rec, store, q)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var exp Explanation
	if err := json.Unmarshal(w.Body.Bytes(), &exp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if exp.Status != "failed" {
		t.Errorf("Status = %q, want failed", exp.Status)
	}
	hadErrHop := false
	for _, h := range exp.AgentHops {
		if h.HadError {
			hadErrHop = true
			if !strings.Contains(h.ErrorText, "deadline exceeded") {
				t.Errorf("ErrorText = %q, want substring 'deadline exceeded'", h.ErrorText)
			}
		}
	}
	if !hadErrHop {
		t.Errorf("expected at least one hop with HadError=true")
	}
}

func TestExplainHandler_HITLDecisionsAndPendingReviews(t *testing.T) {
	rec := audit.NewInMemory()
	store := eval.NewInMemoryStore()
	q := hitl.NewQueue()

	caseID := "case-hitl-001"
	base := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)

	// Decided review (in audit log).
	_ = rec.Record(audit.Event{
		ID:         "evt-rev-1",
		At:         base.Add(10 * time.Second),
		Kind:       audit.KindReviewDecision,
		AgentID:    "human_review",
		CaseID:     caseID,
		Type:       "diagnosis_review",
		Decision:   "approve",
		Reason:     "vitals consistent with diagnosis",
		ReviewerID: "rn-rachel",
	})

	// Pending review (in queue, no audit decision yet).
	q.Submit(medical.ReviewRequest{
		ID:          "rev-pending-1",
		Kind:        "care_plan_review",
		Status:      medical.ReviewPending,
		CaseID:      caseID,
		Priority:    "normal",
		SubmittedAt: base.Add(20 * time.Second),
	})

	req := httptest.NewRequest(http.MethodGet, "/explain/"+caseID, nil)
	req.SetPathValue("case_id", caseID)
	w := httptest.NewRecorder()

	explainHandler("tenant-test", rec, store, q)(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var exp Explanation
	if err := json.Unmarshal(w.Body.Bytes(), &exp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := len(exp.HumanReviews); got != 2 {
		t.Fatalf("HumanReviews len = %d, want 2 (one decided + one pending)", got)
	}
	// Chronologically the decided one is first.
	if exp.HumanReviews[0].Decision != "approve" {
		t.Errorf("first review decision = %q, want approve", exp.HumanReviews[0].Decision)
	}
	if exp.HumanReviews[0].ReviewerID != "rn-rachel" {
		t.Errorf("first reviewer = %q, want rn-rachel", exp.HumanReviews[0].ReviewerID)
	}
	if exp.HumanReviews[1].Status != string(medical.ReviewPending) {
		t.Errorf("second review status = %q, want pending", exp.HumanReviews[1].Status)
	}
}

func TestExplainHandler_PHIFreeFieldSelection(t *testing.T) {
	// Defence in depth: the explanation response must not expose PHI-shaped
	// JSON FIELD KEYS regardless of audit event contents. Checking field
	// keys (not arbitrary substrings) lets the prose disclaimers in the
	// Caveats field talk about narrative/PHI exclusion explicitly without
	// false-positive matching.
	rec := audit.NewInMemory()
	caseID := "case-phi-001"
	_ = rec.Record(audit.Event{
		ID:      "evt-1",
		At:      time.Now().UTC(),
		Kind:    audit.KindMessage,
		AgentID: "diagnostician",
		CaseID:  caseID,
		Type:    "diagnosis_proposal",
	})

	req := httptest.NewRequest(http.MethodGet, "/explain/"+caseID, nil)
	req.SetPathValue("case_id", caseID)
	w := httptest.NewRecorder()

	explainHandler("tenant-test", rec, eval.NewInMemoryStore(), hitl.NewQueue())(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := strings.ToLower(w.Body.String())
	// JSON keys end with `":` so substring matches like `"chief_complaint":`
	// are how the explanation schema would actually leak PHI shape.
	forbiddenKeys := []string{
		`"chief_complaint":`, `"hpi":`, `"patient_name":`, `"dob":`,
		`"mrn":`, `"ssn":`, `"narrative_text":`, `"test_result_value":`,
		`"medication_name":`, `"medication_dose":`,
	}
	for _, k := range forbiddenKeys {
		if strings.Contains(body, k) {
			t.Errorf("response contains forbidden JSON key %q — PHI surface leaked", k)
		}
	}
}

func TestExplainHandler_MissingCaseID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/explain/", nil)
	w := httptest.NewRecorder()

	// PathValue not set — simulates a misrouted request.
	explainHandler("tenant-test", audit.NewInMemory(), eval.NewInMemoryStore(), hitl.NewQueue())(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (missing case_id)", w.Code)
	}
}
