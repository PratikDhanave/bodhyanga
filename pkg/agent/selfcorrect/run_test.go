package selfcorrect_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent/selfcorrect"
	"github.com/PratikDhanave/bodh/pkg/llm"
)

// ----- stub LLM client -----

// callRecord captures one Complete call.
type callRecord struct {
	system  string
	content string // first user message content
}

// sequentialClient returns a fixed sequence of responses in order.
// After exhausting the sequence it returns the last response repeatedly.
type sequentialClient struct {
	responses []string
	calls     []callRecord
	idx       int
}

func (s *sequentialClient) Provider() string { return "stub" }
func (s *sequentialClient) Model() string    { return "stub-model" }
func (s *sequentialClient) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	rec := callRecord{system: req.System}
	if len(req.Messages) > 0 {
		rec.content = req.Messages[len(req.Messages)-1].Content
	}
	s.calls = append(s.calls, rec)

	resp := s.responses[s.idx]
	if s.idx < len(s.responses)-1 {
		s.idx++
	}
	return &llm.Response{
		Content:    resp,
		Provider:   "stub",
		Model:      "stub-model",
		StopReason: "stop",
	}, nil
}

// goodCritiqueJSON returns a JSON critique string with the given score.
func goodCritiqueJSON(score float64, correct bool) string {
	issues := []string{}
	suggestions := []string{}
	if !correct {
		issues = []string{"minor wording issue"}
		suggestions = []string{"rephrase for clarity"}
	}
	b, _ := json.Marshal(map[string]any{
		"is_correct":  correct,
		"issues":      issues,
		"suggestions": suggestions,
		"score":       score,
	})
	return string(b)
}

// ----- tests -----

func TestRunSelfCorrectAgent_ImmediatelyCorrect(t *testing.T) {
	// The very first critique returns score >= threshold → no revision needed.
	stub := &sequentialClient{
		responses: []string{
			"func reverse(s string) string { ... }", // answer
			goodCritiqueJSON(0.95, true),            // critique (above threshold)
		},
	}

	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub,
		"Write a Go function that reverses a string",
		3, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rounds != 1 {
		t.Errorf("want 1 round, got %d", result.Rounds)
	}
	if result.RevisedAnswer != result.OriginalAnswer {
		t.Errorf("no revision expected, but OriginalAnswer != RevisedAnswer")
	}
	if result.Improved {
		t.Errorf("Improved should be false when no revision occurred")
	}
}

func TestRunSelfCorrectAgent_OneRevisionNeeded(t *testing.T) {
	// First critique is poor; revision produces a better answer; second
	// critique is above threshold.
	stub := &sequentialClient{
		responses: []string{
			"bad answer",                   // round 1 answer
			goodCritiqueJSON(0.4, false),   // round 1 critique (below threshold)
			"improved answer",              // revised answer
			goodCritiqueJSON(0.92, true),   // round 2 critique (above threshold)
		},
	}

	var answers, revisions []string
	var critiques []selfcorrect.CritiqueResult

	cb := selfcorrect.SelfCorrectCallbacks{
		OnAnswer:   func(r int, a string) { answers = append(answers, a) },
		OnCritique: func(r int, c selfcorrect.CritiqueResult) { critiques = append(critiques, c) },
		OnRevision: func(r int, rev string) { revisions = append(revisions, rev) },
	}

	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub,
		"Write a Go function that reverses a string",
		3, 0.85, cb,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.OriginalAnswer != "bad answer" {
		t.Errorf("want OriginalAnswer=%q, got %q", "bad answer", result.OriginalAnswer)
	}
	if result.RevisedAnswer != "improved answer" {
		t.Errorf("want RevisedAnswer=%q, got %q", "improved answer", result.RevisedAnswer)
	}
	if !result.Improved {
		t.Errorf("Improved should be true when score increased after revision")
	}
	if result.Rounds != 2 {
		t.Errorf("want 2 rounds, got %d", result.Rounds)
	}

	// Callback counts.
	if len(answers) != 1 {
		t.Errorf("want 1 OnAnswer call (initial), got %d", len(answers))
	}
	if len(critiques) != 2 {
		t.Errorf("want 2 OnCritique calls, got %d", len(critiques))
	}
	if len(revisions) != 1 {
		t.Errorf("want 1 OnRevision call, got %d", len(revisions))
	}
}

func TestRunSelfCorrectAgent_MaxRoundsReached(t *testing.T) {
	// All critiques stay below threshold — agent exhausts maxRounds=2.
	stub := &sequentialClient{
		responses: []string{
			"answer v1",                  // round 1 answer
			goodCritiqueJSON(0.3, false), // round 1 critique (below threshold)
			"answer v2",                  // revision
			goodCritiqueJSON(0.5, false), // round 2 critique (still below threshold)
		},
	}

	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub,
		"some task",
		2, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rounds != 2 {
		t.Errorf("want 2 rounds (max), got %d", result.Rounds)
	}
	// The final revised answer should be v2 (last revision produced).
	if result.RevisedAnswer != "answer v2" {
		t.Errorf("want RevisedAnswer=%q, got %q", "answer v2", result.RevisedAnswer)
	}
}

func TestRunSelfCorrectAgent_DefaultMaxRoundsApplied(t *testing.T) {
	// maxRounds=0 should use the default (3). Feed enough responses to exhaust it.
	responses := []string{
		"initial answer",             // answer
		goodCritiqueJSON(0.2, false), // critique 1
		"revision 1",                 // revision
		goodCritiqueJSON(0.3, false), // critique 2
		"revision 2",                 // revision
		goodCritiqueJSON(0.4, false), // critique 3 (hits maxRounds=3)
	}
	stub := &sequentialClient{responses: responses}

	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub,
		"task",
		0, // use default
		0, // use default threshold
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Rounds != selfcorrect.DefaultMaxRounds {
		t.Errorf("want Rounds=%d (default), got %d", selfcorrect.DefaultMaxRounds, result.Rounds)
	}
}

func TestRunSelfCorrectAgent_OnDoneCallback(t *testing.T) {
	stub := &sequentialClient{
		responses: []string{
			"answer",
			goodCritiqueJSON(0.9, true),
		},
	}

	var doneResult *selfcorrect.RevisionResult
	cb := selfcorrect.SelfCorrectCallbacks{
		OnDone: func(r selfcorrect.RevisionResult) { doneResult = &r },
	}

	result, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 3, 0.85, cb,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doneResult == nil {
		t.Fatal("OnDone was not called")
	}
	if doneResult.RevisedAnswer != result.RevisedAnswer {
		t.Errorf("OnDone result mismatch: want %q, got %q",
			result.RevisedAnswer, doneResult.RevisedAnswer)
	}
}

func TestRunSelfCorrectAgent_CritiqueSystemPromptIsCached(t *testing.T) {
	// The critique call should set CacheSystem=true to save tokens on repeated runs.
	stub := &sequentialClient{
		responses: []string{
			"answer",
			goodCritiqueJSON(0.95, true),
		},
	}
	_, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 1, 0.5,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// call[0] = answer, call[1] = critique
	if len(stub.calls) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(stub.calls))
	}
	critiqueCall := stub.calls[1]
	if !strings.Contains(critiqueCall.system, "strict reviewer") {
		t.Errorf("critique call should use the strict reviewer system prompt")
	}
}

func TestRunSelfCorrectAgent_RevisionPromptIncludesIssues(t *testing.T) {
	// When revising, the user prompt must include the issues from the critique.
	stub := &sequentialClient{
		responses: []string{
			"initial answer",
			`{"is_correct":false,"issues":["missing edge case"],"suggestions":["handle empty string"],"score":0.4}`,
			"revised answer",
			goodCritiqueJSON(0.9, true),
		},
	}

	_, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 3, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// call[0]=answer, call[1]=critique, call[2]=revision, call[3]=critique
	if len(stub.calls) < 3 {
		t.Fatalf("expected at least 3 calls, got %d", len(stub.calls))
	}
	revisionPrompt := stub.calls[2].content
	if !strings.Contains(revisionPrompt, "missing edge case") {
		t.Errorf("revision prompt should contain the issue text, got: %q", revisionPrompt)
	}
	if !strings.Contains(revisionPrompt, "handle empty string") {
		t.Errorf("revision prompt should contain the suggestion text, got: %q", revisionPrompt)
	}
}

func TestRunSelfCorrectAgent_NilCallbacksAreSafe(t *testing.T) {
	stub := &sequentialClient{
		responses: []string{
			"answer",
			goodCritiqueJSON(0.9, true),
		},
	}
	// All callback fields nil — must not panic.
	_, err := selfcorrect.RunSelfCorrectAgent(
		context.Background(), stub, "task", 3, 0.85,
		selfcorrect.SelfCorrectCallbacks{},
	)
	if err != nil {
		t.Fatalf("unexpected error with nil callbacks: %v", err)
	}
}
