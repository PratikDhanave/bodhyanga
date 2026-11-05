package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/audio"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// --- Audio transcriber classifyError edge cases ---

func TestClassifyErrorUnavailable(t *testing.T) {
	got := classifyError(audio.ErrUnavailable)
	if got != "unavailable" {
		t.Errorf("got %q, want unavailable", got)
	}
}

func TestClassifyErrorUnsupportedMedia(t *testing.T) {
	got := classifyError(audio.ErrUnsupportedMedia)
	if got != "unsupported_media" {
		t.Errorf("got %q, want unsupported_media", got)
	}
}

func TestClassifyErrorCanceled(t *testing.T) {
	got := classifyError(context.Canceled)
	if got != "unavailable" {
		t.Errorf("got %q, want unavailable", got)
	}
}

func TestClassifyErrorDeadlineExceeded(t *testing.T) {
	got := classifyError(context.DeadlineExceeded)
	if got != "unavailable" {
		t.Errorf("got %q, want unavailable", got)
	}
}

func TestClassifyErrorInternal(t *testing.T) {
	got := classifyError(errors.New("some random error"))
	if got != "internal" {
		t.Errorf("got %q, want internal", got)
	}
}

// --- Infectious disease selectCAP edge cases ---

func TestSelectCAPMildPenAllergy(t *testing.T) {
	rec := selectCAP("mild", true)
	if rec.Antibiotic != "doxycycline" {
		t.Errorf("antibiotic=%s", rec.Antibiotic)
	}
}

func TestSelectCAPMildNoPenAllergy(t *testing.T) {
	rec := selectCAP("mild", false)
	if rec.Antibiotic != "azithromycin" {
		t.Errorf("antibiotic=%s", rec.Antibiotic)
	}
}

func TestSelectCAPModeratePenAllergy(t *testing.T) {
	rec := selectCAP("moderate", true)
	if rec.Antibiotic != "levofloxacin" {
		t.Errorf("antibiotic=%s", rec.Antibiotic)
	}
}

func TestSelectCAPModerateNoPenAllergy(t *testing.T) {
	rec := selectCAP("moderate", false)
	if !strings.Contains(rec.Antibiotic, "amoxicillin") {
		t.Errorf("antibiotic=%s", rec.Antibiotic)
	}
}

func TestSelectCAPSeverePenAllergy(t *testing.T) {
	rec := selectCAP("severe", true)
	if !strings.Contains(rec.Antibiotic, "aztreonam") {
		t.Errorf("antibiotic=%s", rec.Antibiotic)
	}
}

func TestSelectCAPSevereNoPenAllergy(t *testing.T) {
	rec := selectCAP("severe", false)
	if !strings.Contains(rec.Antibiotic, "piperacillin") {
		t.Errorf("antibiotic=%s", rec.Antibiotic)
	}
}

// --- Infectious disease selectCellulitis edge cases ---

func TestSelectCellulitisMildPenAllergy(t *testing.T) {
	rec := selectCellulitis("mild", true)
	if rec.Antibiotic != "clindamycin" {
		t.Errorf("antibiotic=%s", rec.Antibiotic)
	}
}

func TestSelectCellulitisMildNoPenAllergy(t *testing.T) {
	rec := selectCellulitis("mild", false)
	if rec.Antibiotic != "cephalexin" {
		t.Errorf("antibiotic=%s", rec.Antibiotic)
	}
}

func TestSelectCellulitisSeverePenAllergy(t *testing.T) {
	rec := selectCellulitis("severe", true)
	if !strings.Contains(rec.Antibiotic, "vancomycin") {
		t.Errorf("antibiotic=%s, want vancomycin", rec.Antibiotic)
	}
}

func TestSelectCellulitisSevereNoPenAllergy(t *testing.T) {
	rec := selectCellulitis("severe", false)
	if rec.Antibiotic == "" {
		t.Error("antibiotic should not be empty")
	}
}

// --- extractWBC parse error path ---

func TestExtractWBCParseError(t *testing.T) {
	// The regex should always produce valid numbers, but test defense code.
	_, ok := extractWBC("WBC")
	if ok {
		t.Error("WBC with no number should return false")
	}
}

// --- extractFirstNumber parse error path ---

func TestExtractFirstNumberParsable(t *testing.T) {
	// Normal case: number after marker.
	val, ok := extractFirstNumber("glucose 412 mg/dL", "glucose")
	if !ok || val != 412 {
		t.Errorf("got %v ok=%v", val, ok)
	}
}

// --- Cascade ID test ---

func TestCascadeID(t *testing.T) {
	c := &CascadeDiagnostician{}
	if c.ID() != "diagnostician" {
		t.Errorf("ID=%q", c.ID())
	}
}

// --- Cascade with timeout ---

func TestCascadeWithTimeout(t *testing.T) {
	proposalJSON := `{"diagnosis":"CAP","confidence":0.85,"rationale":"test"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: proposalJSON}}
	a := &CascadeDiagnostician{
		SmallTier: &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		LargeTier: &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}},
		Timeout:   30 * 1e9, // 30s
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

// --- CDM Planner HandleMessage with post_stroke_ischemic ---

func TestCDMPlannerPostStrokeIschemic(t *testing.T) {
	a := &CDMPlannerAgent{}
	body, _ := json.Marshal(cdmRequest{
		PatientID: "p1",
		Condition: "post_stroke_ischemic",
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

// --- CDM Planner with registry update (existing patient) ---

func TestCDMPlannerRegistryUpdate(t *testing.T) {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID:  "p1",
		Conditions: []string{"dm"},
	})
	a := &CDMPlannerAgent{Registry: reg}
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
	rec, ok := reg.Get("p1")
	if !ok {
		t.Fatal("patient should exist")
	}
	// Should now have both conditions.
	found := false
	for _, c := range rec.Conditions {
		if c == "htn" {
			found = true
		}
	}
	if !found {
		t.Error("htn condition should be added")
	}
}

// --- Diagnostician HandleMessage valid case ---

func TestDiagnosticianHandleMessageValid(t *testing.T) {
	a := &DiagnosticianAgent{}
	state := medical.CaseState{
		CaseID:    "c1",
		Condition: "cap",
		TestResults: map[string]string{
			"chest_xray":           "RLL infiltrate",
			"complete_blood_count": "WBC 14.2 K",
		},
	}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].To != "reasoning_verifier" {
		t.Errorf("To=%s", out[0].To)
	}
}

// --- COPD grade edge case ---

func TestCopdGradeNameAll(t *testing.T) {
	cases := []struct {
		grade string
		want  string
	}{
		{"GOLD 1", "Mild"},
		{"GOLD 2", "Moderate"},
		{"GOLD 3", "Severe"},
		{"GOLD 4", "Very Severe"},
		{"unknown", "Unknown"},
		{"", "Unknown"},
	}
	for _, tc := range cases {
		got := copdGradeName(tc.grade)
		if got != tc.want {
			t.Errorf("copdGradeName(%q)=%q, want %q", tc.grade, got, tc.want)
		}
	}
}

// --- Asthma severity edge case ---

func TestAsthmaSeverityAll(t *testing.T) {
	cases := []struct {
		syms string
		want string
	}{
		{"occasional wheezing, rare symptoms", "intermittent"},
		{"wheezing most days, mild persistent", "mild persistent"},
		{"daily asthma symptoms, every day", "moderate persistent"},
		{"continuous wheezing, limited activity, severe persistent", "severe persistent"},
		{"no respiratory issues", ""},
	}
	for _, tc := range cases {
		got := asthmaSeverity(tc.syms)
		if got != tc.want {
			t.Errorf("asthmaSeverity(%q)=%q, want %q", tc.syms, got, tc.want)
		}
	}
}

// --- LLM vision diagnostician nil base ---

func TestVisionDiagnosticianNilBase(t *testing.T) {
	a := &LLMDiagnosticianVision{}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("nil base should error")
	}
}

// --- Test planner invalid JSON ---

func TestTestPlannerInvalidJSON(t *testing.T) {
	a := &TestPlannerAgent{}
	msg := agent.NewMessage("supervisor", "test_planner", agent.RoleAgent,
		"case_state", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

// --- Test planner nil TestResults ---

func TestTestPlannerNilTestResults(t *testing.T) {
	a := &TestPlannerAgent{}
	state := medical.CaseState{CaseID: "c1", Condition: "cap"}
	msg := agent.NewMessage("supervisor", "test_planner", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
}

// --- Intake Name and Capabilities ---

func TestIntakeNameCapabilities(t *testing.T) {
	a := &IntakeAgent{}
	if a.Name() == "" {
		t.Error("Name empty")
	}
	if len(a.Capabilities()) == 0 {
		t.Error("Capabilities empty")
	}
}

// --- Cascade handleMessage with escalation ---

func TestCascadeEscalation(t *testing.T) {
	// Small tier returns low confidence, triggering escalation.
	lowConfJSON := `{"diagnosis":"maybe CAP","confidence":0.35,"rationale":"uncertain"}`
	highConfJSON := `{"diagnosis":"CAP","confidence":0.90,"rationale":"clear infiltrate"}`
	smallMock := &llm.Mock{Resp: &llm.Response{Content: lowConfJSON}}
	largeMock := &llm.Mock{Resp: &llm.Response{Content: highConfJSON}}
	a := &CascadeDiagnostician{
		SmallTier:  &LLMDiagnostician{Client: smallMock, Fallback: &DiagnosticianAgent{}},
		LargeTier:  &LLMDiagnostician{Client: largeMock, Fallback: &DiagnosticianAgent{}},
		EscalateAt: 0.6,
	}
	state := medical.CaseState{CaseID: "c1", Condition: "cap"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 1 {
		t.Error("should produce output from large tier")
	}
}

// --- LLM diagnostician completeAndDecode empty diagnosis ---

func TestLLMDiagnosticianEmptyDiagnosis(t *testing.T) {
	respJSON := `{"diagnosis":"","confidence":0.91,"rationale":"test"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	// Should fall back due to empty diagnosis.
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	// Should get fallback output.
	if len(out) < 1 {
		t.Error("should produce fallback output")
	}
}

// --- LLM diagnostician completeAndDecode low confidence ---

func TestLLMDiagnosticianBelowMinConfidence(t *testing.T) {
	respJSON := `{"diagnosis":"CAP","confidence":0.1,"rationale":"test"}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON}}
	a := &LLMDiagnostician{Client: mock, Fallback: &DiagnosticianAgent{}}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 1 {
		t.Error("should produce fallback output")
	}
}
