package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func newQuestionerState(t *testing.T) (medical.CaseState, string) {
	t.Helper()
	state := medical.CaseState{
		CaseID:       "test-case-questioner",
		Presentation: "Productive cough and fever for 4 days",
		Condition:    "cap",
	}
	return state, medical.EncodeState(state)
}

func TestLLMQuestionerSuccessEmitsExpectedMessage(t *testing.T) {
	state, encoded := newQuestionerState(t)
	respJSON := `{"questions":[` +
		`{"q":"How long has the cough lasted?","a":"4 days."},` +
		`{"q":"Any fever or chills?","a":"Yes, fever to 38.6C."},` +
		`{"q":"Any sick contacts?","a":"No known sick contacts."}` +
		`],"hypotheses":["community_acquired_pneumonia","viral_uri","acute_bronchitis"]}`
	mock := &llm.Mock{Resp: &llm.Response{Content: respJSON, InputTokens: 80, OutputTokens: 60}}
	q := &LLMQuestioner{Client: mock, Fallback: &QuestionerAgent{}}

	msg := agent.NewMessage("diagnostic_supervisor", "questioner", agent.RoleAgent, "case_state", encoded,
		map[string]any{"case_id": state.CaseID})
	env := newFakeEnv()

	out, err := q.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if out[0].To != "diagnostic_supervisor" {
		t.Fatalf("To=%s, want diagnostic_supervisor", out[0].To)
	}
	if out[0].Type != "questions_complete" {
		t.Fatalf("Type=%s, want questions_complete", out[0].Type)
	}
	got, err := medical.DecodeState(out[0].Content)
	if err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(got.QuestionsAsked) != 3 {
		t.Fatalf("QuestionsAsked=%d, want 3", len(got.QuestionsAsked))
	}
	if got.Answers["How long has the cough lasted?"] != "4 days." {
		t.Fatalf("missing answer for question 1: %+v", got.Answers)
	}
	if len(got.Hypotheses) != 3 || got.Hypotheses[0] != "community_acquired_pneumonia" {
		t.Fatalf("Hypotheses=%v", got.Hypotheses)
	}
	if len(mock.Calls) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(mock.Calls))
	}
	if mock.Calls[0].JSONSchema == nil {
		t.Fatalf("expected JSON schema to be sent to LLM")
	}
	if !mock.Calls[0].CacheSystem {
		t.Fatalf("expected CacheSystem=true on system prompt")
	}
}

func TestLLMQuestionerFallsBackOnLLMError(t *testing.T) {
	_, encoded := newQuestionerState(t)
	mock := &llm.Mock{Err: errors.New("network down")}
	q := &LLMQuestioner{Client: mock, Fallback: &QuestionerAgent{}}

	msg := agent.NewMessage("diagnostic_supervisor", "questioner", agent.RoleAgent, "case_state", encoded,
		map[string]any{"case_id": "x"})
	env := newFakeEnv()

	out, err := q.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "diagnostic_supervisor" || out[0].Type != "questions_complete" {
		t.Fatalf("fallback should emit questions_complete to supervisor: %+v", out)
	}
	if !containsLog(env, "falling back") {
		t.Fatalf("expected fallback log line, got %v", env.logs)
	}
}

func TestLLMQuestionerFallsBackOnBadJSON(t *testing.T) {
	_, encoded := newQuestionerState(t)
	mock := &llm.Mock{Resp: &llm.Response{Content: "this is not json"}}
	q := &LLMQuestioner{Client: mock, Fallback: &QuestionerAgent{}}

	msg := agent.NewMessage("diagnostic_supervisor", "questioner", agent.RoleAgent, "case_state", encoded,
		map[string]any{"case_id": "x"})
	out, err := q.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "diagnostic_supervisor" || out[0].Type != "questions_complete" {
		t.Fatalf("bad JSON should trigger fallback emission: %+v", out)
	}
}
