package debate

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// positionJSON builds a minimal valid positionResponse JSON string.
func positionJSON(argument string, confidence float64) string {
	b, _ := json.Marshal(positionResponse{Argument: argument, Confidence: confidence})
	return string(b)
}

// synthesisJSON builds a minimal valid synthesisResponse JSON string.
func synthesisJSON(synthesis string, confidence float64, reasoning string) string {
	b, _ := json.Marshal(synthesisResponse{Synthesis: synthesis, Confidence: confidence, Reasoning: reasoning})
	return string(b)
}

// sequentialMock is an llm.Client that returns a series of responses in order.
// The first N calls return positionJSON; the (N+1)th returns synthesisJSON.
type sequentialMock struct {
	mu        sync.Mutex // debaters call Complete concurrently
	responses []string
	idx       int
	calls     []llm.Request
}

func (s *sequentialMock) Provider() string { return "mock" }
func (s *sequentialMock) Model() string    { return "mock-model" }
func (s *sequentialMock) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	content := ""
	if s.idx < len(s.responses) {
		content = s.responses[s.idx]
	}
	s.idx++
	return &llm.Response{Content: content}, nil
}

func TestRunDebateAgent_HappyPath(t *testing.T) {
	debaters := []Debater{
		{Name: "Optimist", Perspective: "You are an optimist."},
		{Name: "Critic", Perspective: "You are a critic."},
	}

	// Two debater responses + one chair synthesis response.
	// We can't predict which order goroutines complete, so we accept any order
	// and just validate the final result shape.
	mock := &sequentialMock{
		responses: []string{
			positionJSON("This will work great", 0.9),
			positionJSON("There are significant risks", 0.7),
			synthesisJSON("Consider both benefits and risks before deciding", 0.8, "Balanced view"),
		},
	}

	var positionCalls []string
	var synthesisCalls []string

	result, err := RunDebateAgent(context.Background(), mock, "Test question?", debaters, DebateCallbacks{
		OnPosition: func(debater, argument string) {
			positionCalls = append(positionCalls, debater)
		},
		OnSynthesis: func(r string) {
			synthesisCalls = append(synthesisCalls, r)
		},
	})

	if err != nil {
		t.Fatalf("RunDebateAgent returned error: %v", err)
	}

	if result.Question != "Test question?" {
		t.Errorf("Question = %q, want %q", result.Question, "Test question?")
	}

	if len(result.Positions) != 2 {
		t.Errorf("len(Positions) = %d, want 2", len(result.Positions))
	}

	if result.Synthesis == "" {
		t.Error("Synthesis is empty, want non-empty")
	}

	if result.Confidence <= 0 || result.Confidence > 1 {
		t.Errorf("Confidence = %f, want in (0,1]", result.Confidence)
	}

	// Both debaters should have fired OnPosition.
	if len(positionCalls) != 2 {
		t.Errorf("OnPosition called %d times, want 2", len(positionCalls))
	}

	// Synthesis should have fired once.
	if len(synthesisCalls) != 1 {
		t.Errorf("OnSynthesis called %d times, want 1", len(synthesisCalls))
	}

	// Total LLM calls: 2 debaters + 1 chair = 3.
	if len(mock.calls) != 3 {
		t.Errorf("LLM called %d times, want 3", len(mock.calls))
	}
}

func TestRunDebateAgent_NilCallbacks(t *testing.T) {
	debaters := []Debater{
		{Name: "A", Perspective: "You are agent A."},
	}

	mock := &sequentialMock{
		responses: []string{
			positionJSON("A's argument", 0.6),
			synthesisJSON("Synthesis of A", 0.6, "Only one debater"),
		},
	}

	// Should not panic with zero-value callbacks.
	result, err := RunDebateAgent(context.Background(), mock, "Q?", debaters, DebateCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Positions) != 1 {
		t.Errorf("len(Positions) = %d, want 1", len(result.Positions))
	}
}

func TestRunDebateAgent_DefaultDebaters(t *testing.T) {
	defaults := DefaultDebaters()
	if len(defaults) < 2 {
		t.Fatalf("DefaultDebaters() returned %d, want >= 2", len(defaults))
	}
	for _, d := range defaults {
		if d.Name == "" {
			t.Error("DefaultDebater has empty Name")
		}
		if d.Perspective == "" {
			t.Error("DefaultDebater has empty Perspective")
		}
	}
}

func TestRunDebateAgent_FallbackOnBadJSON(t *testing.T) {
	debaters := []Debater{
		{Name: "Fallback", Perspective: "You are a fallback agent."},
	}

	// Return plain text instead of JSON — should be handled gracefully.
	mock := &sequentialMock{
		responses: []string{
			"I think microservices are the way to go.",
			synthesisJSON("Synthesis", 0.5, "Used fallback"),
		},
	}

	result, err := RunDebateAgent(context.Background(), mock, "Monolith or microservices?", debaters, DebateCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error on bad JSON: %v", err)
	}

	if len(result.Positions) != 1 {
		t.Errorf("len(Positions) = %d, want 1", len(result.Positions))
	}
	// The raw text should end up as the argument.
	if !strings.Contains(result.Positions[0].Argument, "microservices") {
		t.Errorf("Argument = %q, want it to contain the raw text", result.Positions[0].Argument)
	}
}

func TestRunDebateAgent_ConfidenceClamped(t *testing.T) {
	debaters := []Debater{
		{Name: "Extreme", Perspective: "You always return extreme values."},
	}

	// Return out-of-range confidence values.
	b, _ := json.Marshal(positionResponse{Argument: "arg", Confidence: 1.5})
	sb, _ := json.Marshal(synthesisResponse{Synthesis: "syn", Confidence: -0.1, Reasoning: "r"})

	mock := &sequentialMock{
		responses: []string{string(b), string(sb)},
	}

	result, err := RunDebateAgent(context.Background(), mock, "Q?", debaters, DebateCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Positions[0].Confidence > 1 {
		t.Errorf("Position Confidence = %f, should be clamped to <= 1", result.Positions[0].Confidence)
	}
	if result.Confidence < 0 {
		t.Errorf("Synthesis Confidence = %f, should be clamped to >= 0", result.Confidence)
	}
}

func TestRunDebateAgent_AllFail(t *testing.T) {
	debaters := []Debater{
		{Name: "Broken", Perspective: "You are broken."},
	}

	// Return an error from the LLM.
	mock := &llm.Mock{Err: errTestLLM}

	_, err := RunDebateAgent(context.Background(), mock, "Q?", debaters, DebateCallbacks{})
	if err == nil {
		t.Fatal("expected error when all debaters fail, got nil")
	}
}

// errTestLLM is a sentinel error for tests.
var errTestLLM = &testError{"mock LLM error"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
