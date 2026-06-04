package selfcorrect_test

import (
	"context"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent/selfcorrect"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

// --- errorClient always fails ---

type errorClient struct {
	callIdx   int
	failAfter int // fail starting from this call index (0-based)
}

func (e *errorClient) Provider() string { return "err" }
func (e *errorClient) Model() string    { return "err-model" }
func (e *errorClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	idx := e.callIdx
	e.callIdx++
	if idx >= e.failAfter {
		return nil, errors.New("llm error")
	}
	return &llm.Response{Content: "ok", Provider: "err", Model: "err-model"}, nil
}

// --- Initial answer LLM failure ---

func TestRunSelfCorrectAgent_InitialAnswerFails(t *testing.T) {
	client := &errorClient{failAfter: 0}
	_, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), client, "task", 3, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err == nil {
		t.Fatal("expected error when initial answer fails")
	}
}

// --- Critique returns invalid JSON (fallback path) ---

func TestRunSelfCorrectAgent_CritiqueInvalidJSON(t *testing.T) {
	// answer call succeeds, critique call returns non-JSON
	stub := &sequentialClient{
		responses: []string{
			"my answer",      // initial answer
			"not valid json", // critique - will fail JSON parse
		},
	}
	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 3, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// When critique fails to parse, it returns a CritiqueResult with score=0
	// and an error. The agent should break out of the loop (best-effort).
	if result.RevisedAnswer != "my answer" {
		t.Errorf("RevisedAnswer = %q, want %q (original kept on critique failure)", result.RevisedAnswer, "my answer")
	}
}

// --- Critique LLM call itself fails ---

func TestRunSelfCorrectAgent_CritiqueLLMFails(t *testing.T) {
	// answer succeeds, then critique call errors
	client := &errorClient{failAfter: 1} // fail on call index 1 (critique)
	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), client, "task", 3, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// On critique failure the agent keeps the current answer and stops.
	if result.RevisedAnswer != "ok" {
		t.Errorf("RevisedAnswer = %q, want %q", result.RevisedAnswer, "ok")
	}
}

// --- Revision LLM call fails (keeps current answer) ---

// failOnCallNClient returns specific responses for the first N calls, then errors.
type failOnCallNClient struct {
	responses []string
	idx       int
}

func (f *failOnCallNClient) Provider() string { return "stub" }
func (f *failOnCallNClient) Model() string    { return "stub-model" }
func (f *failOnCallNClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	if f.idx >= len(f.responses) {
		return nil, errors.New("call limit exceeded")
	}
	resp := f.responses[f.idx]
	f.idx++
	return &llm.Response{Content: resp, Provider: "stub", Model: "stub-model", StopReason: "stop"}, nil
}

func TestRunSelfCorrectAgent_RevisionFails(t *testing.T) {
	// Call 0 = answer, Call 1 = critique (below threshold), Call 2 = revision (fails)
	client := &failOnCallNClient{
		responses: []string{
			"initial answer",
			goodCritiqueJSON(0.3, false), // below threshold, will trigger revision
			// No 3rd response -> revision call fails
		},
	}

	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), client, "task", 3, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// On revision failure the agent keeps the current answer and stops.
	if result.RevisedAnswer != "initial answer" {
		t.Errorf("RevisedAnswer = %q, want %q (kept on revision failure)", result.RevisedAnswer, "initial answer")
	}
}

// --- scoreThreshold > 1 defaults ---

func TestRunSelfCorrectAgent_ScoreThresholdAboveOne(t *testing.T) {
	stub := &sequentialClient{
		responses: []string{
			"answer",
			goodCritiqueJSON(0.9, true), // will pass default threshold (0.85)
		},
	}
	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 3, 1.5, // invalid threshold
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rounds != 1 {
		t.Errorf("Rounds = %d, want 1", result.Rounds)
	}
}

// --- IsCorrect=true causes early stop ---

func TestRunSelfCorrectAgent_IsCorrectStopsEarly(t *testing.T) {
	stub := &sequentialClient{
		responses: []string{
			"answer",
			goodCritiqueJSON(0.5, true), // low score but is_correct=true
		},
	}
	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 5, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rounds != 1 {
		t.Errorf("Rounds = %d, want 1 (is_correct=true should stop)", result.Rounds)
	}
}

// --- OnDone called on max rounds exhaustion ---

func TestRunSelfCorrectAgent_OnDoneCalledOnMaxRounds(t *testing.T) {
	stub := &sequentialClient{
		responses: []string{
			"answer",
			goodCritiqueJSON(0.3, false), // below threshold
		},
	}
	var doneCalled bool
	cb := selfcorrect.SelfCorrectCallbacks{
		OnDone: func(r selfcorrect.RevisionResult) { doneCalled = true },
	}
	_, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 1, 0.85, cb,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !doneCalled {
		t.Error("OnDone should be called when max rounds exhausted")
	}
}

// --- Critique with nil issues/suggestions gets normalized ---

func TestRunSelfCorrectAgent_CritiqueNilSlicesNormalized(t *testing.T) {
	stub := &sequentialClient{
		responses: []string{
			"answer",
			`{"is_correct":true,"issues":null,"suggestions":null,"score":0.95}`,
		},
	}
	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 3, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Critique.Issues == nil {
		t.Error("Issues should be normalized to empty slice, not nil")
	}
	if result.Critique.Suggestions == nil {
		t.Error("Suggestions should be normalized to empty slice, not nil")
	}
}
