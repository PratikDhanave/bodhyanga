package multiturneval

import (
	"context"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// mockClient is a test double for llm.Client that returns pre-configured
// responses (or errors) in sequence and captures every request for assertion.
type mockClient struct {
	responses []*llm.Response
	errors    []error
	idx       int
	captured  []llm.Request
}

func (m *mockClient) Provider() string { return "mock" }
func (m *mockClient) Model() string    { return "mock-model" }

func (m *mockClient) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	m.captured = append(m.captured, req)
	i := m.idx
	m.idx++
	if i < len(m.errors) && m.errors[i] != nil {
		return nil, m.errors[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return &llm.Response{Content: "default"}, nil
}

// ---------------------------------------------------------------------------
// 1. RunMultiTurnEval — single case, 2 turns
// ---------------------------------------------------------------------------

func TestRunMultiTurnEval_SingleCase_TwoTurns(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{Content: "hello from turn 1"},
			{Content: "hello from turn 2"},
		},
	}
	cases := []MultiTurnCase{
		{
			Name: "two-turn case",
			Turns: []ConversationTurn{
				{UserMessage: "hi"},
				{UserMessage: "how are you"},
			},
		},
	}

	results, err := RunMultiTurnEval(context.Background(), cases, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if len(r.TurnResults) != 2 {
		t.Fatalf("expected 2 TurnResults, got %d", len(r.TurnResults))
	}
	if r.FinalOutput != "hello from turn 2" {
		t.Errorf("FinalOutput = %q, want %q", r.FinalOutput, "hello from turn 2")
	}
	if r.TurnResults[0].Output != "hello from turn 1" {
		t.Errorf("turn 1 output = %q, want %q", r.TurnResults[0].Output, "hello from turn 1")
	}
	if r.TurnResults[1].Output != "hello from turn 2" {
		t.Errorf("turn 2 output = %q, want %q", r.TurnResults[1].Output, "hello from turn 2")
	}
}

// ---------------------------------------------------------------------------
// 2. RunMultiTurnEval — multiple cases
// ---------------------------------------------------------------------------

func TestRunMultiTurnEval_MultipleCases(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{Content: "resp-case1"},
			{Content: "resp-case2"},
		},
	}
	cases := []MultiTurnCase{
		{Name: "case-1", Turns: []ConversationTurn{{UserMessage: "a"}}},
		{Name: "case-2", Turns: []ConversationTurn{{UserMessage: "b"}}},
	}

	results, err := RunMultiTurnEval(context.Background(), cases, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Case.Name != "case-1" {
		t.Errorf("first result case name = %q, want %q", results[0].Case.Name, "case-1")
	}
	if results[1].Case.Name != "case-2" {
		t.Errorf("second result case name = %q, want %q", results[1].Case.Name, "case-2")
	}
}

// ---------------------------------------------------------------------------
// 3. RunMultiTurnEval — empty cases
// ---------------------------------------------------------------------------

func TestRunMultiTurnEval_EmptyCases(t *testing.T) {
	client := &mockClient{}
	results, err := RunMultiTurnEval(context.Background(), nil, client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// 4. runOne — with CheckFn pass (returns "")
// ---------------------------------------------------------------------------

func TestRunOne_CheckFnPass(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{{Content: "good answer"}},
	}
	c := MultiTurnCase{
		Name: "check-pass",
		Turns: []ConversationTurn{
			{
				UserMessage: "question",
				CheckFn:     func(output string) string { return "" },
			},
		},
	}

	result := runOne(context.Background(), c, client)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.TurnResults) != 1 {
		t.Fatalf("expected 1 TurnResult, got %d", len(result.TurnResults))
	}
	if result.TurnResults[0].Check != "" {
		t.Errorf("Check = %q, want empty", result.TurnResults[0].Check)
	}
}

// ---------------------------------------------------------------------------
// 5. runOne — with CheckFn fail (returns "bad")
// ---------------------------------------------------------------------------

func TestRunOne_CheckFnFail(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{{Content: "wrong answer"}},
	}
	c := MultiTurnCase{
		Name: "check-fail",
		Turns: []ConversationTurn{
			{
				UserMessage: "question",
				CheckFn:     func(output string) string { return "bad" },
			},
		},
	}

	result := runOne(context.Background(), c, client)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.TurnResults[0].Check != "bad" {
		t.Errorf("Check = %q, want %q", result.TurnResults[0].Check, "bad")
	}
}

// ---------------------------------------------------------------------------
// 6. runOne — LLM error on turn 2
// ---------------------------------------------------------------------------

func TestRunOne_LLMError(t *testing.T) {
	llmErr := errors.New("connection refused")
	client := &mockClient{
		responses: []*llm.Response{{Content: "turn 1 ok"}},
		errors:    []error{nil, llmErr},
	}
	c := MultiTurnCase{
		Name: "llm-error",
		Turns: []ConversationTurn{
			{UserMessage: "first"},
			{UserMessage: "second"},
		},
	}

	result := runOne(context.Background(), c, client)
	if result.Err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(result.Err, llmErr) {
		t.Errorf("expected error wrapping %v, got %v", llmErr, result.Err)
	}
	// Turn 1 succeeded + turn 2 recorded with the error = 2 TurnResults.
	if len(result.TurnResults) != 2 {
		t.Fatalf("expected 2 TurnResults, got %d", len(result.TurnResults))
	}
	// Turn 1 should have output; turn 2 should have empty output.
	if result.TurnResults[0].Output != "turn 1 ok" {
		t.Errorf("turn 1 output = %q, want %q", result.TurnResults[0].Output, "turn 1 ok")
	}
	if result.TurnResults[1].Output != "" {
		t.Errorf("turn 2 output = %q, want empty", result.TurnResults[1].Output)
	}
}

// ---------------------------------------------------------------------------
// 7. runOne — custom system prompt
// ---------------------------------------------------------------------------

func TestRunOne_CustomSystemPrompt(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{{Content: "ok"}},
	}
	custom := "You are a medical diagnostician."
	c := MultiTurnCase{
		Name:   "custom-system",
		System: custom,
		Turns:  []ConversationTurn{{UserMessage: "diagnose me"}},
	}

	runOne(context.Background(), c, client)

	if len(client.captured) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(client.captured))
	}
	if client.captured[0].System != custom {
		t.Errorf("System = %q, want %q", client.captured[0].System, custom)
	}
}

// ---------------------------------------------------------------------------
// 8. runOne — default system prompt (empty System field)
// ---------------------------------------------------------------------------

func TestRunOne_DefaultSystemPrompt(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{{Content: "ok"}},
	}
	c := MultiTurnCase{
		Name:  "default-system",
		Turns: []ConversationTurn{{UserMessage: "hello"}},
	}

	runOne(context.Background(), c, client)

	if len(client.captured) != 1 {
		t.Fatalf("expected 1 captured request, got %d", len(client.captured))
	}
	if client.captured[0].System != SystemPrompt {
		t.Errorf("System = %q, want default %q", client.captured[0].System, SystemPrompt)
	}
}

// ---------------------------------------------------------------------------
// 9. runOne — FinalEvaluators with 2 evaluators
// ---------------------------------------------------------------------------

func TestRunOne_FinalEvaluators(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{{Content: "final output"}},
	}
	c := MultiTurnCase{
		Name:  "evaluators",
		Turns: []ConversationTurn{{UserMessage: "hello"}},
		FinalEvaluators: map[string]func(output string) float64{
			"length": func(output string) float64 {
				if len(output) > 5 {
					return 1.0
				}
				return 0.0
			},
			"contains_final": func(output string) float64 {
				if output == "final output" {
					return 0.75
				}
				return 0.0
			},
		},
	}

	result := runOne(context.Background(), c, client)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.Scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(result.Scores))
	}
	if result.Scores["length"] != 1.0 {
		t.Errorf("length score = %f, want 1.0", result.Scores["length"])
	}
	if result.Scores["contains_final"] != 0.75 {
		t.Errorf("contains_final score = %f, want 0.75", result.Scores["contains_final"])
	}
}

// ---------------------------------------------------------------------------
// 10. runOne — no evaluators
// ---------------------------------------------------------------------------

func TestRunOne_NoEvaluators(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{{Content: "ok"}},
	}
	c := MultiTurnCase{
		Name:  "no-evaluators",
		Turns: []ConversationTurn{{UserMessage: "hello"}},
	}

	result := runOne(context.Background(), c, client)
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if len(result.Scores) != 0 {
		t.Errorf("expected empty Scores map, got %d entries", len(result.Scores))
	}
}

// ---------------------------------------------------------------------------
// 11. runOne — conversation history accumulation
// ---------------------------------------------------------------------------

func TestRunOne_HistoryAccumulation(t *testing.T) {
	client := &mockClient{
		responses: []*llm.Response{
			{Content: "answer 1"},
			{Content: "answer 2"},
		},
	}
	c := MultiTurnCase{
		Name: "history",
		Turns: []ConversationTurn{
			{UserMessage: "question 1"},
			{UserMessage: "question 2"},
		},
	}

	runOne(context.Background(), c, client)

	if len(client.captured) != 2 {
		t.Fatalf("expected 2 captured requests, got %d", len(client.captured))
	}

	// Turn 1: only one user message.
	req1 := client.captured[0]
	if len(req1.Messages) != 1 {
		t.Fatalf("turn 1: expected 1 message, got %d", len(req1.Messages))
	}
	if req1.Messages[0].Role != llm.RoleUser || req1.Messages[0].Content != "question 1" {
		t.Errorf("turn 1 msg[0] = %v, want user/question 1", req1.Messages[0])
	}

	// Turn 2: user1, assistant1, user2 = 3 messages.
	req2 := client.captured[1]
	if len(req2.Messages) != 3 {
		t.Fatalf("turn 2: expected 3 messages, got %d", len(req2.Messages))
	}
	if req2.Messages[0].Role != llm.RoleUser || req2.Messages[0].Content != "question 1" {
		t.Errorf("turn 2 msg[0] = %v, want user/question 1", req2.Messages[0])
	}
	if req2.Messages[1].Role != llm.RoleAssistant || req2.Messages[1].Content != "answer 1" {
		t.Errorf("turn 2 msg[1] = %v, want assistant/answer 1", req2.Messages[1])
	}
	if req2.Messages[2].Role != llm.RoleUser || req2.Messages[2].Content != "question 2" {
		t.Errorf("turn 2 msg[2] = %v, want user/question 2", req2.Messages[2])
	}
}

// ---------------------------------------------------------------------------
// 12. PrintMultiTurnReport — no panic
// ---------------------------------------------------------------------------

func TestPrintMultiTurnReport_NoPanic(t *testing.T) {
	results := []MultiTurnResult{
		{
			Case: MultiTurnCase{Name: "pass-case"},
			TurnResults: []TurnResult{
				{Turn: 1, Input: "hi", Output: "hello", Check: ""},
			},
			FinalOutput: "hello",
			Scores:      map[string]float64{"quality": 0.9},
		},
		{
			Case: MultiTurnCase{Name: "fail-case"},
			Err:  errors.New("something broke"),
			TurnResults: []TurnResult{
				{Turn: 1, Input: "hi", Output: "", Check: "something broke"},
			},
			Scores: map[string]float64{},
		},
		{
			Case: MultiTurnCase{Name: "no-scores"},
			TurnResults: []TurnResult{
				{Turn: 1, Input: "hi", Output: "world", Check: ""},
			},
			FinalOutput: "world",
			Scores:      map[string]float64{},
		},
	}

	// Verify it does not panic.
	PrintMultiTurnReport(results)
}
