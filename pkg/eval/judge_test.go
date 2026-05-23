package eval

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

func sampleItem() GradeItem {
	return GradeItem{
		Scenario: "demo-case-001",
		Expected: "Acute decompensated heart failure",
		Actual:   "ADHF with reduced ejection fraction",
		Context:  "Elderly patient with crackles and elevated BNP.",
	}
}

func TestLLMJudge_HappyPath(t *testing.T) {
	mock := &llm.Mock{
		ProviderName: "mock",
		ModelName:    "judge-test",
		Resp: &llm.Response{
			Provider: "mock",
			Model:    "judge-test",
			Content: `{
                "score": 0.85,
                "pass": true,
                "rationale": "Equivalent label and good differential.",
                "dimensional_scores": {
                    "correctness": 0.9,
                    "specificity": 0.8,
                    "safety": 0.85,
                    "differential_quality": 0.85
                }
            }`,
		},
	}
	j := NewLLMJudge(mock)

	g, err := j.Grade(context.Background(), sampleItem())
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if g.Score < 0.84 || g.Score > 0.86 {
		t.Fatalf("score: got %v want ~0.85", g.Score)
	}
	if !g.Pass {
		t.Fatalf("expected Pass=true at score %v", g.Score)
	}
	if !strings.Contains(g.Rationale, "Equivalent") {
		t.Fatalf("rationale not propagated: %q", g.Rationale)
	}
	if g.DimensionalScores["correctness"] != 0.9 {
		t.Fatalf("dimensional correctness: %v", g.DimensionalScores["correctness"])
	}
	if g.Provider != "mock" || g.Model != "judge-test" {
		t.Fatalf("provider/model: %s/%s", g.Provider, g.Model)
	}
}

func TestLLMJudge_FailingScoreSetsPassFalse(t *testing.T) {
	mock := &llm.Mock{
		Resp: &llm.Response{
			Provider: "mock",
			Model:    "m",
			Content: `{
                "score": 0.4,
                "pass": true,
                "rationale": "Diagnosis too vague.",
                "dimensional_scores": {
                    "correctness": 0.5,
                    "specificity": 0.2,
                    "safety": 0.5,
                    "differential_quality": 0.4
                }
            }`,
		},
	}
	j := NewLLMJudge(mock)
	g, err := j.Grade(context.Background(), sampleItem())
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if g.Pass {
		t.Fatalf("expected Pass=false at score %v even though model said true", g.Score)
	}
}

func TestLLMJudge_LLMErrorBubblesUp(t *testing.T) {
	want := errors.New("boom")
	mock := &llm.Mock{Err: want}
	j := NewLLMJudge(mock)
	_, err := j.Grade(context.Background(), sampleItem())
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected wrapped %v, got %v", want, err)
	}
}

func TestLLMJudge_InvalidJSONReturnsError(t *testing.T) {
	mock := &llm.Mock{
		Resp: &llm.Response{Provider: "mock", Model: "m", Content: "not json at all"},
	}
	j := NewLLMJudge(mock)
	_, err := j.Grade(context.Background(), sampleItem())
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("expected invalid JSON error, got %v", err)
	}
}

func TestLLMJudge_ClampsScoreToZeroOneRange(t *testing.T) {
	mock := &llm.Mock{
		Resp: &llm.Response{
			Provider: "mock", Model: "m",
			Content: `{
                "score": 1.5,
                "pass": true,
                "rationale": "over-the-top model",
                "dimensional_scores": {
                    "correctness": 1.5,
                    "specificity": -0.2,
                    "safety": 0.9,
                    "differential_quality": 0.8
                }
            }`,
		},
	}
	j := NewLLMJudge(mock)
	g, err := j.Grade(context.Background(), sampleItem())
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if g.Score != 1.0 {
		t.Fatalf("expected score clamped to 1.0, got %v", g.Score)
	}
	if g.DimensionalScores["correctness"] != 1.0 {
		t.Fatalf("correctness clamp: %v", g.DimensionalScores["correctness"])
	}
	if g.DimensionalScores["specificity"] != 0.0 {
		t.Fatalf("specificity clamp: %v", g.DimensionalScores["specificity"])
	}

	// Negative on the top-level score should clamp to 0.
	mock.Resp.Content = `{
        "score": -0.2,
        "pass": false,
        "rationale": "below zero",
        "dimensional_scores": {
            "correctness": -0.5,
            "specificity": 0,
            "safety": 0,
            "differential_quality": 0
        }
    }`
	g, err = j.Grade(context.Background(), sampleItem())
	if err != nil {
		t.Fatalf("Grade negative: %v", err)
	}
	if g.Score != 0 {
		t.Fatalf("expected negative score clamped to 0, got %v", g.Score)
	}
	if g.Pass {
		t.Fatalf("expected Pass=false at zero score")
	}
}

func TestLLMJudge_UsesCachedSystemPrompt(t *testing.T) {
	mock := &llm.Mock{
		Resp: &llm.Response{Provider: "mock", Model: "m", Content: `{
            "score": 0.7, "pass": true, "rationale": "ok",
            "dimensional_scores": {"correctness":0.7,"specificity":0.7,"safety":0.7,"differential_quality":0.7}
        }`},
	}
	j := NewLLMJudge(mock)
	if _, err := j.Grade(context.Background(), sampleItem()); err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected exactly 1 call, got %d", len(mock.Calls))
	}
	got := mock.Calls[0]
	if !got.CacheSystem {
		t.Fatalf("expected CacheSystem=true on Request, got false")
	}
	if !strings.Contains(got.System, "clinical") {
		t.Fatalf("system prompt does not look like the rubric: %q", got.System)
	}
	if got.JSONSchema == nil || got.JSONSchema.Name != "clinical_grade" {
		t.Fatalf("expected JSONSchema name 'clinical_grade', got %+v", got.JSONSchema)
	}
}

func TestLLMJudge_RubricOverride(t *testing.T) {
	mock := &llm.Mock{
		Resp: &llm.Response{Provider: "mock", Model: "m", Content: `{
            "score":0.5,"pass":false,"rationale":"meh",
            "dimensional_scores":{"correctness":0.5,"specificity":0.5,"safety":0.5,"differential_quality":0.5}
        }`},
	}
	j := NewLLMJudge(mock)
	item := sampleItem()
	item.Rubric = "Custom rubric: only correctness matters."
	if _, err := j.Grade(context.Background(), item); err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if mock.Calls[0].System != item.Rubric {
		t.Fatalf("rubric override not applied; system: %q", mock.Calls[0].System)
	}
}

func TestBatchGrade_RunsConcurrentlyAcrossItems(t *testing.T) {
	const n = 12
	items := make([]GradeItem, n)
	for i := range items {
		items[i] = sampleItem()
	}

	mock := &countingJudge{}
	grades, errs := BatchGrade(context.Background(), mock, items)
	if len(grades) != n || len(errs) != n {
		t.Fatalf("expected %d grades/errs, got %d/%d", n, len(grades), len(errs))
	}
	for i := range items {
		if errs[i] != nil {
			t.Fatalf("err[%d]: %v", i, errs[i])
		}
		if grades[i] == nil {
			t.Fatalf("grade[%d] nil", i)
		}
	}
	if mock.calls != n {
		t.Fatalf("expected judge called %d times, got %d", n, mock.calls)
	}
	if mock.maxInFlight < 2 {
		t.Fatalf("expected concurrent execution, maxInFlight=%d", mock.maxInFlight)
	}
	if mock.maxInFlight > 4 {
		t.Fatalf("worker pool should cap at 4, saw %d in flight", mock.maxInFlight)
	}
}

func TestBatchGrade_EmptyInput(t *testing.T) {
	grades, errs := BatchGrade(context.Background(), &countingJudge{}, nil)
	if len(grades) != 0 || len(errs) != 0 {
		t.Fatalf("expected empty slices, got %d/%d", len(grades), len(errs))
	}
}

// countingJudge is a Judge that records concurrency and call counts so
// BatchGrade tests can assert the worker pool actually fans out.
type countingJudge struct {
	mu          sync.Mutex
	calls       int
	inFlight    int
	maxInFlight int
}

func (c *countingJudge) Grade(ctx context.Context, _ GradeItem) (*Grade, error) {
	c.mu.Lock()
	c.calls++
	c.inFlight++
	if c.inFlight > c.maxInFlight {
		c.maxInFlight = c.inFlight
	}
	c.mu.Unlock()

	// Sleep so other workers actually pick up jobs and lift maxInFlight.
	// A busy loop is unreliable: the Go runtime can finish all 1000 iterations
	// on a single goroutine before any other gets scheduled.
	time.Sleep(20 * time.Millisecond)

	c.mu.Lock()
	c.inFlight--
	c.mu.Unlock()
	return &Grade{Score: 0.7, Pass: true, Rationale: "ok"}, nil
}
