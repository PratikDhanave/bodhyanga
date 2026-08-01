package debate

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// --- stub LLM client for debate tests ---

type debateStubClient struct {
	mu        sync.Mutex // debaters call Complete concurrently
	responses []string
	idx       int
	err       error // if set, all calls fail
}

func (s *debateStubClient) Provider() string { return "stub" }
func (s *debateStubClient) Model() string    { return "stub-model" }
func (s *debateStubClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.mu.Lock()
	resp := s.responses[s.idx]
	if s.idx < len(s.responses)-1 {
		s.idx++
	}
	s.mu.Unlock()
	return &llm.Response{Content: resp, Provider: "stub", Model: "stub-model"}, nil
}

// --- callDebater JSON unmarshal fallback ---

func TestCallDebater_JSONUnmarshalFallback(t *testing.T) {
	stub := &debateStubClient{responses: []string{"This is not JSON at all"}}
	pos, err := callDebater(context.Background(), stub, "test question", Debater{
		Name:        "test-debater",
		Perspective: "test perspective",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.Confidence != 0.5 {
		t.Errorf("fallback confidence = %f, want 0.5", pos.Confidence)
	}
	if pos.Argument != "This is not JSON at all" {
		t.Errorf("fallback argument = %q, want raw response", pos.Argument)
	}
}

// --- callDebater confidence clamping ---

func TestCallDebater_ConfidenceClampLow(t *testing.T) {
	resp, _ := json.Marshal(positionResponse{Argument: "test", Confidence: -0.5})
	stub := &debateStubClient{responses: []string{string(resp)}}
	pos, err := callDebater(context.Background(), stub, "q", Debater{Name: "d", Perspective: "p"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.Confidence != 0 {
		t.Errorf("confidence = %f, want 0 (clamped)", pos.Confidence)
	}
}

func TestCallDebater_ConfidenceClampHigh(t *testing.T) {
	resp, _ := json.Marshal(positionResponse{Argument: "test", Confidence: 1.5})
	stub := &debateStubClient{responses: []string{string(resp)}}
	pos, err := callDebater(context.Background(), stub, "q", Debater{Name: "d", Perspective: "p"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pos.Confidence != 1.0 {
		t.Errorf("confidence = %f, want 1.0 (clamped)", pos.Confidence)
	}
}

// --- callDebater LLM error ---

func TestCallDebater_LLMError(t *testing.T) {
	stub := &debateStubClient{err: errors.New("llm fail")}
	_, err := callDebater(context.Background(), stub, "q", Debater{Name: "d", Perspective: "p"})
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

// --- callChair JSON unmarshal fallback ---

func TestCallChair_JSONFallback(t *testing.T) {
	stub := &debateStubClient{responses: []string{"not json"}}
	positions := []Position{
		{Debater: "d1", Argument: "arg1", Confidence: 0.8},
	}
	synth, conf, err := callChair(context.Background(), stub, "q", positions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if synth != "not json" {
		t.Errorf("synthesis = %q, want raw content", synth)
	}
	if conf != 0.5 {
		t.Errorf("fallback confidence = %f, want 0.5", conf)
	}
}

// --- callChair confidence clamping ---

func TestCallChair_ConfidenceClampLow(t *testing.T) {
	resp, _ := json.Marshal(synthesisResponse{Synthesis: "s", Confidence: -1, Reasoning: "r"})
	stub := &debateStubClient{responses: []string{string(resp)}}
	positions := []Position{{Debater: "d1", Argument: "a", Confidence: 0.5}}
	_, conf, err := callChair(context.Background(), stub, "q", positions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conf != 0 {
		t.Errorf("confidence = %f, want 0 (clamped)", conf)
	}
}

func TestCallChair_ConfidenceClampHigh(t *testing.T) {
	resp, _ := json.Marshal(synthesisResponse{Synthesis: "s", Confidence: 2.0, Reasoning: "r"})
	stub := &debateStubClient{responses: []string{string(resp)}}
	positions := []Position{{Debater: "d1", Argument: "a", Confidence: 0.5}}
	_, conf, err := callChair(context.Background(), stub, "q", positions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conf != 1.0 {
		t.Errorf("confidence = %f, want 1.0 (clamped)", conf)
	}
}

// --- callChair LLM error ---

func TestCallChair_LLMError(t *testing.T) {
	stub := &debateStubClient{err: errors.New("llm fail")}
	positions := []Position{{Debater: "d1", Argument: "a", Confidence: 0.5}}
	_, _, err := callChair(context.Background(), stub, "q", positions)
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
}

// --- RunDebateAgent: all debaters fail ---

func TestRunDebateAgent_AllDebatersFail(t *testing.T) {
	stub := &debateStubClient{err: errors.New("all fail")}
	_, err := RunDebateAgent(context.Background(), stub, "q", DefaultDebaters(), DebateCallbacks{})
	if err == nil {
		t.Fatal("expected error when all debaters fail")
	}
}

// --- RunDebateAgent: partial debater failure with callbacks ---

func TestRunDebateAgent_PartialFailureWithCallbacks(t *testing.T) {
	goodResp, _ := json.Marshal(positionResponse{Argument: "good arg", Confidence: 0.8})
	synthResp, _ := json.Marshal(synthesisResponse{Synthesis: "synthesis", Confidence: 0.9, Reasoning: "reason"})

	sc := &debateStubClient{responses: []string{string(goodResp), string(synthResp)}}

	var posNames []string
	var synthResult string
	cb := DebateCallbacks{
		OnPosition:  func(name, arg string) { posNames = append(posNames, name) },
		OnSynthesis: func(s string) { synthResult = s },
	}

	// Use just one debater to simplify
	debaters := []Debater{{Name: "d1", Perspective: "perspective 1"}}
	result, err := RunDebateAgent(context.Background(), sc, "question", debaters, cb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(posNames) < 1 {
		t.Errorf("expected at least 1 position callback, got %d", len(posNames))
	}
	if synthResult == "" {
		t.Error("expected synthesis callback to be called")
	}
	if result.Question != "question" {
		t.Errorf("Question = %q, want %q", result.Question, "question")
	}
}

// --- RunDebateAgent: empty debaters uses defaults ---

func TestRunDebateAgent_EmptyDebatersUsesDefaults(t *testing.T) {
	goodResp, _ := json.Marshal(positionResponse{Argument: "arg", Confidence: 0.7})
	synthResp, _ := json.Marshal(synthesisResponse{Synthesis: "synth", Confidence: 0.8, Reasoning: "r"})
	stub := &debateStubClient{responses: []string{string(goodResp), string(synthResp)}}
	// Pass nil debaters - should use DefaultDebaters()
	result, err := RunDebateAgent(context.Background(), stub, "q", nil, DebateCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Positions) == 0 {
		t.Error("expected positions from default debaters")
	}
}

// --- RunDebateAgent: chair synthesis fails ---

func TestRunDebateAgent_ChairSynthesisFails(t *testing.T) {
	// We need a client where debater calls succeed but the chair call fails.
	// Use a counting approach.
	goodResp, _ := json.Marshal(positionResponse{Argument: "arg", Confidence: 0.7})

	callCount := 0
	client := &failAfterNClient{
		successResp: string(goodResp),
		failAfter:   1, // First call succeeds (debater), second call fails (chair)
	}
	_ = callCount

	debaters := []Debater{{Name: "d1", Perspective: "p1"}}
	_, err := RunDebateAgent(context.Background(), client, "q", debaters, DebateCallbacks{})
	if err == nil {
		t.Fatal("expected error when chair synthesis fails")
	}
}

type failAfterNClient struct {
	successResp string
	failAfter   int
	count       int
}

func (f *failAfterNClient) Provider() string { return "stub" }
func (f *failAfterNClient) Model() string    { return "stub" }
func (f *failAfterNClient) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	f.count++
	if f.count > f.failAfter {
		return nil, errors.New("chair fail")
	}
	return &llm.Response{Content: f.successResp}, nil
}

// --- RunDebateAgent: nil callbacks safe ---

func TestRunDebateAgent_NilCallbacksSafe(t *testing.T) {
	goodResp, _ := json.Marshal(positionResponse{Argument: "arg", Confidence: 0.7})
	synthResp, _ := json.Marshal(synthesisResponse{Synthesis: "synth", Confidence: 0.8, Reasoning: "r"})
	stub := &debateStubClient{responses: []string{string(goodResp), string(synthResp)}}
	debaters := []Debater{{Name: "d1", Perspective: "p1"}}
	_, err := RunDebateAgent(context.Background(), stub, "q", debaters, DebateCallbacks{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
