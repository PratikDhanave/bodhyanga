package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// TestSplitCSV_BasicAndEmpty exercises the comma/space tokenizer used to
// parse the -providers flag. Empty input must return an empty slice (so we
// can len()==0 check it); trimmed tokens are preserved verbatim.
func TestSplitCSV_BasicAndEmpty(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"rule", []string{"rule"}},
		{"rule,llm-anthropic", []string{"rule", "llm-anthropic"}},
		{"rule, llm-openai , llm-ollama", []string{"rule", "llm-openai", "llm-ollama"}},
		{",,,", []string{}},
		{"  ", []string{}},
	}
	for _, c := range cases {
		got := splitCSV(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitCSV(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestRunCaseWithRuleEmitsFinalState drives the CAP fixture through the
// real pipeline with the rule-based diagnostician and asserts the captured
// CaseState carries the expected working diagnosis. This is the smoke test
// that confirms the bench wiring still works end-to-end.
func TestRunCaseWithRuleEmitsFinalState(t *testing.T) {
	f := fixture{
		CaseID:         "demo-case-001",
		ChiefComplaint: "Cough and fever",
		HPI:            "58-year-old presents with 5 days productive cough and fever to 38.9°C",
		Demographics:   "58M",
		Vitals:         "T 38.9°C, HR 98, RR 22, SpO2 94% on room air",
		GoldDiagnosis:  "Community-acquired pneumonia",
		MaxCostUSD:     500,
	}
	diagFactory := func() (agent.Agent, error) { return &agents.DiagnosticianAgent{}, nil }
	state, ok := runCase(f, 3*time.Second, diagFactory)
	if !ok {
		t.Fatalf("runCase did not finalize")
	}
	if state.CaseID != f.CaseID {
		t.Errorf("CaseID = %q, want %q", state.CaseID, f.CaseID)
	}
	if !strings.Contains(strings.ToLower(state.WorkingDiagnosis), "pneumonia") {
		t.Errorf("WorkingDiagnosis = %q, want contains 'pneumonia'", state.WorkingDiagnosis)
	}
}

// TestSummarize_EmptyAndPopulated checks both branches of the summarize
// helper: empty input yields the zero bench struct; populated input yields
// the arithmetic mean of accuracy, cost, and under-budget proportion.
func TestSummarize_EmptyAndPopulated(t *testing.T) {
	if got := summarize(nil); got != (bench{}) {
		t.Errorf("summarize(nil) = %+v, want zero", got)
	}
	results := []result{
		{Metrics: eval.CaseMetrics{Accuracy: 1.0, CostUSD: 100, UnderBudget: true}},
		{Metrics: eval.CaseMetrics{Accuracy: 0.5, CostUSD: 300, UnderBudget: false}},
	}
	got := summarize(results)
	want := bench{Cases: 2, MeanAccuracyPct: 75, MeanCostUSD: 200, UnderBudgetPct: 50}
	if got != want {
		t.Errorf("summarize() = %+v, want %+v", got, want)
	}
}

// TestJudgeGate_FailsBelowThreshold builds two synthetic providerRuns —
// one with mean judge score 0.4, the other 0.8 — and asserts judgePassFail
// flags only the first when threshold = 0.6.
func TestJudgeGate_FailsBelowThreshold(t *testing.T) {
	runs := []providerRun{
		{
			Provider: "llm-low",
			Results: []result{
				{JudgeGrade: &eval.Grade{Score: 0.3}},
				{JudgeGrade: &eval.Grade{Score: 0.5}},
			},
		},
		{
			Provider: "llm-high",
			Results: []result{
				{JudgeGrade: &eval.Grade{Score: 0.7}},
				{JudgeGrade: &eval.Grade{Score: 0.9}},
			},
		},
	}
	failing := judgePassFail(runs, 0.6)
	want := []string{"llm-low"}
	if !reflect.DeepEqual(failing, want) {
		t.Errorf("judgePassFail = %v, want %v", failing, want)
	}
	if got := providerJudgeMean(runs[0].Results); got != 0.4 {
		t.Errorf("providerJudgeMean(low) = %v, want 0.4", got)
	}
	if got := providerJudgeMean(runs[1].Results); got != 0.8 {
		t.Errorf("providerJudgeMean(high) = %v, want 0.8", got)
	}
}

// TestJudgeGate_PassesAtOrAboveThreshold verifies the gate is inclusive on
// the boundary: exactly 0.6 counts as passing.
func TestJudgeGate_PassesAtOrAboveThreshold(t *testing.T) {
	runs := []providerRun{
		{
			Provider: "llm-boundary",
			Results: []result{
				{JudgeGrade: &eval.Grade{Score: 0.6}},
				{JudgeGrade: &eval.Grade{Score: 0.6}},
			},
		},
	}
	failing := judgePassFail(runs, 0.6)
	if len(failing) != 0 {
		t.Errorf("judgePassFail at boundary = %v, want []", failing)
	}
}

// TestJudgeGate_SkipsProvidersWithNoGrades ensures providers that ran but
// produced no judge grades (e.g. judge errored on every case) do not silently
// fail the gate — they're skipped, mirroring the runtime behaviour where a
// missing JudgeGrade renders as "–".
func TestJudgeGate_SkipsProvidersWithNoGrades(t *testing.T) {
	runs := []providerRun{
		{
			Provider: "llm-empty",
			Results: []result{
				{JudgeGrade: nil},
				{JudgeGrade: nil},
			},
		},
	}
	if failing := judgePassFail(runs, 0.6); len(failing) != 0 {
		t.Errorf("judgePassFail with no grades = %v, want []", failing)
	}
}

// TestBuildJudgeFromFlag_UnknownProviderReturnsError covers the validation
// path — rule and bogus strings must both error out cleanly. The success
// branches require live LLM credentials and are exercised in the integration
// suite (not here).
func TestBuildJudgeFromFlag_UnknownProviderReturnsError(t *testing.T) {
	cases := []string{"", "rule", "llm-bogus", "anthropic"}
	for _, p := range cases {
		if _, err := buildJudgeFromFlag(p, ""); err == nil {
			t.Errorf("buildJudgeFromFlag(%q) error = nil, want non-nil", p)
		}
	}
}

// TestAttachJudgeGrades_RoutesThroughJudgeInterface wires a fake Judge —
// powered by an llm.Mock returning canned JSON — and verifies attachJudgeGrades
// populates JudgeGrade on every result. This also exercises the GradeItem
// shape (scenario/expected/actual/context) we build per case.
func TestAttachJudgeGrades_RoutesThroughJudgeInterface(t *testing.T) {
	mockResp := &llm.Response{
		Content: `{"score":0.85,"pass":true,"rationale":"matches","dimensional_scores":{"correctness":0.9,"specificity":0.8,"safety":0.9,"differential_quality":0.8}}`,
	}
	mock := &llm.Mock{Resp: mockResp}
	judge := eval.NewLLMJudge(mock)

	results := []result{
		{
			Provider: "rule",
			Fixture:  fixture{GoldDiagnosis: "Community-acquired pneumonia"},
			State: medical.CaseState{
				CaseID:           "demo-case-001",
				WorkingDiagnosis: "Community-acquired pneumonia",
				Condition:        "cap",
				TotalCostUSD:     345.0,
			},
		},
	}
	attachJudgeGrades(context.Background(), judge, results)
	if results[0].JudgeGrade == nil {
		t.Fatalf("JudgeGrade not attached")
	}
	if results[0].JudgeGrade.Score != 0.85 {
		t.Errorf("Score = %v, want 0.85", results[0].JudgeGrade.Score)
	}
	if len(mock.Calls) != 1 {
		t.Errorf("mock.Calls = %d, want 1", len(mock.Calls))
	}
	// Confirm the user-prompt carried gold + working diagnosis (route through
	// the eval.Judge interface, but make sure GradeItem fields flow through).
	user := mock.Calls[0].Messages[0].Content
	if !strings.Contains(user, "Community-acquired pneumonia") {
		t.Errorf("prompt missing expected gold diagnosis: %q", user)
	}
	if !strings.Contains(user, "case condition: cap") {
		t.Errorf("prompt missing context: %q", user)
	}
}

// TestAttachJudgeGrades_JudgeErrorLeavesGradeNil — when the judge errors on
// a case the corresponding JudgeGrade stays nil and the run continues; the
// gate skips it (covered by TestJudgeGate_SkipsProvidersWithNoGrades).
func TestAttachJudgeGrades_JudgeErrorLeavesGradeNil(t *testing.T) {
	judge := &fakeJudge{err: errors.New("boom")}
	results := []result{{Provider: "rule", Fixture: fixture{CaseID: "x"}}}
	attachJudgeGrades(context.Background(), judge, results)
	if results[0].JudgeGrade != nil {
		t.Errorf("JudgeGrade = %+v, want nil after error", results[0].JudgeGrade)
	}
}

// fakeJudge is a minimal eval.Judge for the error-path test.
type fakeJudge struct {
	err error
}

func (f *fakeJudge) Grade(ctx context.Context, item eval.GradeItem) (*eval.Grade, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &eval.Grade{Score: 1.0, Pass: true}, nil
}

