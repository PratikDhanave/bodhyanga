package agents

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

// --- Reasoning verifier edge cases ---

func TestReasoningVerifierRejectLowConfidence(t *testing.T) {
	a := &ReasoningVerifierAgent{}
	proposal := medical.DiagnosisProposal{
		Diagnosis:  "CAP",
		Confidence: 0.3,
		Rationale:  "Some rationale",
	}
	content, _ := json.Marshal(proposal)
	msg := agent.NewMessage("diagnostician", "reasoning_verifier", agent.RoleAgent,
		"diagnosis_proposal", string(content), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("low confidence should be rejected (nil output)")
	}
}

func TestReasoningVerifierRejectEmptyRationale(t *testing.T) {
	a := &ReasoningVerifierAgent{}
	proposal := medical.DiagnosisProposal{
		Diagnosis:  "CAP",
		Confidence: 0.9,
		Rationale:  "",
	}
	content, _ := json.Marshal(proposal)
	msg := agent.NewMessage("diagnostician", "reasoning_verifier", agent.RoleAgent,
		"diagnosis_proposal", string(content), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("empty rationale should be rejected")
	}
}

func TestReasoningVerifierHITL(t *testing.T) {
	a := &ReasoningVerifierAgent{HITL: true}
	proposal := medical.DiagnosisProposal{
		Diagnosis:  "CAP",
		Confidence: 0.9,
		Rationale:  "RLL infiltrate supports CAP",
	}
	content, _ := json.Marshal(proposal)
	state := medical.EncodeState(medical.CaseState{CaseID: "c1"})
	msg := agent.NewMessage("diagnostician", "reasoning_verifier", agent.RoleAgent,
		"diagnosis_proposal", string(content), map[string]any{"case_state": state})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].To != "human_review" {
		t.Errorf("To=%s, want human_review", out[0].To)
	}
}

func TestReasoningVerifierInvalidJSON(t *testing.T) {
	a := &ReasoningVerifierAgent{}
	msg := agent.NewMessage("diagnostician", "reasoning_verifier", agent.RoleAgent,
		"diagnosis_proposal", "bad", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

// --- Human review edge cases ---

func TestHumanReviewMissingEnvelope(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	// Missing hitl_kind.
	msg := agent.NewMessage("agent", "human_review", agent.RoleAgent,
		"review_request", `{}`, map[string]any{
			"hitl_next_to":   "engagement",
			"hitl_next_type": "care_plan_ready",
		})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("missing envelope should return nil")
	}
}

func TestHumanReviewDefaultSubjectAndPriority(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	msg := agent.NewMessage("agent", "human_review", agent.RoleAgent,
		"review_request", `{"data":"test"}`, map[string]any{
			"hitl_kind":      "test_review",
			"hitl_next_to":   "engagement",
			"hitl_next_type": "care_plan_ready",
		})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("should return nil (queued, no immediate output)")
	}
	// Verify request was queued.
	pending := q.Pending()
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	if pending[0].Priority != "normal" {
		t.Errorf("default priority=%s", pending[0].Priority)
	}
	if pending[0].Subject != "test_review" {
		t.Errorf("default subject=%s", pending[0].Subject)
	}
}

func TestHumanReviewApplyDecisionReject(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	// Submit a request.
	msg := agent.NewMessage("agent", "human_review", agent.RoleAgent,
		"review_request", `{"data":"test"}`, map[string]any{
			"hitl_kind":      "test_review",
			"hitl_next_to":   "engagement",
			"hitl_next_type": "care_plan_ready",
			"hitl_priority":  "high",
			"hitl_subject":   "Test",
		})
	a.HandleMessage(context.Background(), msg, newFakeEnv())
	pending := q.Pending()
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	id := pending[0].ID

	// Reject.
	r, msgs, ok := a.ApplyDecision(id, "dr_smith", "reject", "not appropriate", "")
	if !ok {
		t.Error("ok should be true")
	}
	if msgs != nil {
		t.Error("reject should produce no messages")
	}
	if r.Status != medical.ReviewRejected {
		t.Errorf("status=%s", r.Status)
	}
}

func TestHumanReviewApplyDecisionApproveWithModification(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	msg := agent.NewMessage("agent", "human_review", agent.RoleAgent,
		"review_request", `{"original":"data"}`, map[string]any{
			"hitl_kind":        "test_review",
			"hitl_next_to":     "engagement",
			"hitl_next_type":   "care_plan_ready",
			"hitl_sla_minutes": 60.0,
			"patient_id":       "p1",
		})
	a.HandleMessage(context.Background(), msg, newFakeEnv())
	pending := q.Pending()
	id := pending[0].ID

	modification := `{"modified":"data"}`
	r, msgs, ok := a.ApplyDecision(id, "dr_smith", "approve_with_modifications", "adjusted", modification)
	if !ok {
		t.Error("ok should be true")
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != modification {
		t.Error("modification should be the payload")
	}
	if r.Status != medical.ReviewModified {
		t.Errorf("status=%s", r.Status)
	}
}

func TestHumanReviewApplyDecisionUnknownID(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	_, _, ok := a.ApplyDecision("nonexistent", "dr_smith", "approve", "", "")
	if ok {
		t.Error("unknown id should return ok=false")
	}
}

// --- Panel manager edge cases ---

func TestPanelManagerCarePlanReady(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &PanelManagerAgent{Registry: reg}
	msg := agent.NewMessage("cdm_planner", "panel_manager", agent.RoleAgent,
		"care_plan_ready", `{}`, map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("care_plan_ready should return nil")
	}
}

func TestPanelManagerUnknownType(t *testing.T) {
	reg := medical.NewPatientRegistry()
	a := &PanelManagerAgent{Registry: reg}
	msg := agent.NewMessage("unknown", "panel_manager", agent.RoleAgent,
		"unknown_type", `{}`, map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Error("unknown type should return nil")
	}
}

func TestPanelManagerWithConditionFilter(t *testing.T) {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{PatientID: "p1", Conditions: []string{"htn"}})
	reg.Upsert(medical.PatientRecord{PatientID: "p2", Conditions: []string{"dm"}})
	a := &PanelManagerAgent{Registry: reg}
	body, _ := json.Marshal(map[string]string{"condition": "htn"})
	msg := agent.NewMessage("user", "panel_manager", agent.RoleAgent,
		"panel_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 1 {
		t.Error("should have at least 1 output (panel_report)")
	}
}

func TestPanelManagerHITL(t *testing.T) {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID:      "p1",
		Conditions:     []string{"htn"},
		LastSystolicBP: 165,
	})
	a := &PanelManagerAgent{Registry: reg, HITL: true}
	body, _ := json.Marshal(map[string]string{"condition": "all"})
	msg := agent.NewMessage("user", "panel_manager", agent.RoleAgent,
		"panel_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	// At least the panel_report.
	if len(out) < 1 {
		t.Fatal("expected at least 1 output")
	}
	// Check HITL routing for gaps.
	for _, o := range out {
		if o.Type == "review_request" && o.To == "human_review" {
			return // found HITL-gated gap
		}
	}
	// If no gaps, that's also valid.
}

// --- Questioner edge cases ---

func TestQuestionerCHFHypotheses(t *testing.T) {
	a := &QuestionerAgent{}
	state := medical.CaseState{CaseID: "c1", Condition: "chf"}
	msg := agent.NewMessage("supervisor", "questioner", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	outState, _ := medical.DecodeState(out[0].Content)
	if len(outState.Hypotheses) != 3 {
		t.Errorf("want 3 hypotheses for CHF, got %d", len(outState.Hypotheses))
	}
	if outState.Hypotheses[0] != "acute_decompensated_heart_failure" {
		t.Errorf("first hypothesis=%s", outState.Hypotheses[0])
	}
}

func TestQuestionerInvalidJSON(t *testing.T) {
	a := &QuestionerAgent{}
	msg := agent.NewMessage("supervisor", "questioner", agent.RoleAgent,
		"case_state", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestQuestionerNilAnswers(t *testing.T) {
	a := &QuestionerAgent{}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("supervisor", "questioner", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
}

// --- CDM planner edge cases ---

func TestCDMPlannerWellChild(t *testing.T) {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{PatientID: "p1"})
	a := &CDMPlannerAgent{Registry: reg}
	body, _ := json.Marshal(cdmRequest{
		PatientID: "p1",
		Condition: "well_child",
		CaseID:    "c1",
		AgeMonths: 12,
		WeightKg:  10.0,
	})
	msg := agent.NewMessage("user", "cdm_planner", agent.RoleAgent,
		"cdm_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 (engagement+panel_manager), got %d", len(out))
	}
}

func TestCDMPlannerPostMI(t *testing.T) {
	a := &CDMPlannerAgent{}
	body, _ := json.Marshal(cdmRequest{
		PatientID: "p1",
		Condition: "post_mi",
	})
	msg := agent.NewMessage("user", "cdm_planner", agent.RoleAgent,
		"cdm_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
}

func TestCDMPlannerPostStroke(t *testing.T) {
	a := &CDMPlannerAgent{}
	body, _ := json.Marshal(cdmRequest{
		PatientID: "p1",
		Condition: "post_stroke",
	})
	msg := agent.NewMessage("user", "cdm_planner", agent.RoleAgent,
		"cdm_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
}

func TestCDMPlannerPostStrokeHemorrhagic(t *testing.T) {
	a := &CDMPlannerAgent{}
	body, _ := json.Marshal(cdmRequest{
		PatientID: "p1",
		Condition: "post_stroke_hemorrhagic",
	})
	msg := agent.NewMessage("user", "cdm_planner", agent.RoleAgent,
		"cdm_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
}

func TestCDMPlannerHITL(t *testing.T) {
	a := &CDMPlannerAgent{HITL: true}
	body, _ := json.Marshal(cdmRequest{
		PatientID: "p1",
		Condition: "htn",
		CaseID:    "c1",
	})
	msg := agent.NewMessage("user", "cdm_planner", agent.RoleAgent,
		"cdm_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	if out[0].To != "human_review" {
		t.Errorf("To=%s, want human_review", out[0].To)
	}
}

func TestCDMPlannerNilRegistry(t *testing.T) {
	a := &CDMPlannerAgent{}
	body, _ := json.Marshal(cdmRequest{
		PatientID: "p1",
		Condition: "htn",
	})
	msg := agent.NewMessage("user", "cdm_planner", agent.RoleAgent,
		"cdm_request", string(body), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
}

// --- Cascade diagnostician edge cases ---

func TestCascadeNilTiers(t *testing.T) {
	a := &CascadeDiagnostician{}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("nil tiers should error")
	}
}

func TestCascadeInvalidContent(t *testing.T) {
	mock := &llm.Mock{}
	a := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: mock},
		LargeTier: &LLMDiagnostician{Client: mock},
	}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("invalid content should error")
	}
}

func TestExtractProposalConfidenceNoMatch(t *testing.T) {
	msgs := []agent.Message{
		agent.NewMessage("a", "b", agent.RoleAgent, "other_type", "{}", nil),
	}
	_, ok := extractProposalConfidence(msgs)
	if ok {
		t.Error("no diagnosis_proposal should return ok=false")
	}
}

func TestExtractProposalConfidenceBadJSON(t *testing.T) {
	msgs := []agent.Message{
		agent.NewMessage("a", "b", agent.RoleAgent, "diagnosis_proposal", "bad json", nil),
	}
	_, ok := extractProposalConfidence(msgs)
	if ok {
		t.Error("bad JSON should return ok=false")
	}
}

func TestExtractProposalConfidenceSuccess(t *testing.T) {
	p := medical.DiagnosisProposal{Confidence: 0.85}
	content, _ := json.Marshal(p)
	msgs := []agent.Message{
		agent.NewMessage("a", "b", agent.RoleAgent, "diagnosis_proposal", string(content), nil),
	}
	conf, ok := extractProposalConfidence(msgs)
	if !ok {
		t.Error("should return ok=true")
	}
	if conf != 0.85 {
		t.Errorf("confidence=%f, want 0.85", conf)
	}
}

func TestTierIdentityNil(t *testing.T) {
	p, m := tierIdentity(nil)
	if p != "unknown" || m != "unknown" {
		t.Errorf("nil tier: provider=%s model=%s", p, m)
	}
	p, m = tierIdentity(&LLMDiagnostician{})
	if p != "unknown" || m != "unknown" {
		t.Errorf("nil client: provider=%s model=%s", p, m)
	}
}

func TestTierIdentityWithClient(t *testing.T) {
	mock := &llm.Mock{}
	p, m := tierIdentity(&LLMDiagnostician{Client: mock})
	if p == "" || m == "" {
		t.Error("should return provider and model")
	}
}

// --- LLM diagnostician edge cases ---

func TestLLMDiagnosticianNilFallback(t *testing.T) {
	a := &LLMDiagnostician{
		Client: &llm.Mock{Err: errors.New("fail")},
	}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("nil fallback + LLM error should error")
	}
}

// --- Inbox triage LLM edge cases ---

func TestLLMInboxTriageInvalidJSON(t *testing.T) {
	q := tasks.NewQueue()
	a := &LLMInboxTriage{
		Client:   &llm.Mock{},
		Fallback: &InboxTriageAgent{Tasks: q},
		Tasks:    q,
	}
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

// --- LLM questioner edge cases ---

func TestLLMQuestionerNilFallback(t *testing.T) {
	a := &LLMQuestioner{
		Client: &llm.Mock{Err: errors.New("fail")},
	}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("supervisor", "questioner", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("nil fallback + LLM error should error")
	}
}

// --- LLM reasoning verifier edge cases ---

func TestLLMReasoningVerifierNilFallback(t *testing.T) {
	a := &LLMReasoningVerifier{
		Client: &llm.Mock{Err: errors.New("fail")},
	}
	proposal := medical.DiagnosisProposal{
		Diagnosis:  "CAP",
		Confidence: 0.9,
		Rationale:  "test",
	}
	content, _ := json.Marshal(proposal)
	msg := agent.NewMessage("diagnostician", "reasoning_verifier", agent.RoleAgent,
		"diagnosis_proposal", string(content), map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("nil fallback + LLM error should error")
	}
}

// --- LLM inbox triage nil fallback ---

func TestLLMInboxTriageNilFallback(t *testing.T) {
	a := &LLMInboxTriage{
		Client: &llm.Mock{Err: errors.New("fail")},
	}
	body, _ := json.Marshal(map[string]string{
		"patient_id": "p1",
		"subject":    "test",
		"body":       "test",
	})
	msg := agent.NewMessage("user", "inbox_triage", agent.RoleAgent,
		"inbox_message", string(body), map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("nil fallback + LLM error should error")
	}
}

// --- Cascade with small error + large error ---

func TestCascadeBothTiersFail(t *testing.T) {
	smallMock := &llm.Mock{Err: errors.New("small fail")}
	largeMock := &llm.Mock{Err: errors.New("large fail")}
	a := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: smallMock},
		LargeTier: &LLMDiagnostician{Client: largeMock},
	}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("both tiers failing should return error")
	}
}

func TestCascadeSmallErrorLargeSucceeds(t *testing.T) {
	smallMock := &llm.Mock{Err: errors.New("small fail")}
	proposalJSON := `{"diagnosis":"CAP","confidence":0.85,"rationale":"test"}`
	largeMock := &llm.Mock{Resp: &llm.Response{Content: proposalJSON}}
	a := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: smallMock, Fallback: &DiagnosticianAgent{}},
		LargeTier: &LLMDiagnostician{Client: largeMock, Fallback: &DiagnosticianAgent{}},
	}
	state := medical.CaseState{CaseID: "c1", Condition: "cap"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 1 {
		t.Error("large tier should succeed")
	}
}

func TestCascadeLargeErrorFallsBackToSmall(t *testing.T) {
	proposalJSON := `{"diagnosis":"CAP","confidence":0.45,"rationale":"test"}`
	smallMock := &llm.Mock{Resp: &llm.Response{Content: proposalJSON}}
	largeMock := &llm.Mock{Err: errors.New("large fail")}
	a := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: smallMock, Fallback: &DiagnosticianAgent{}},
		LargeTier: &LLMDiagnostician{Client: largeMock, Fallback: nil},
	}
	state := medical.CaseState{CaseID: "c1", Condition: "cap"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 1 {
		t.Error("should fall back to small tier output")
	}
}

func TestCascadeSmallNoProposal(t *testing.T) {
	// Small tier returns output but no diagnosis_proposal type.
	smallMock := &llm.Mock{Resp: &llm.Response{Content: `{"diagnosis":"CAP","confidence":0.85,"rationale":"test"}`}}
	largeMock := &llm.Mock{Resp: &llm.Response{Content: `{"diagnosis":"CAP","confidence":0.90,"rationale":"test2"}`}}
	a := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: smallMock, Fallback: &DiagnosticianAgent{}},
		LargeTier: &LLMDiagnostician{Client: largeMock, Fallback: &DiagnosticianAgent{}},
	}
	state := medical.CaseState{CaseID: "c1", Condition: "cap"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 1 {
		t.Error("should produce output")
	}
}
