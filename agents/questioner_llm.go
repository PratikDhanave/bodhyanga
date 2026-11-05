package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// LLMQuestioner is a drop-in replacement for QuestionerAgent that asks an LLM
// for context-relevant clinical history questions plus a starting differential.
// On any failure (timeout, JSON parse, transport error, empty questions list)
// it delegates to a rule-based fallback so the pipeline always advances to the
// diagnostic supervisor.
//
// The agent's ID is "questioner" — same as the rule-based one — so the
// supervisor's routing (which targets that ID) does not have to change.
type LLMQuestioner struct {
	// Client is the LLM provider. Required.
	Client llm.Client

	// Fallback runs when the LLM call fails or returns junk. Required.
	// Typically &QuestionerAgent{}.
	Fallback agent.Agent

	// Timeout caps a single LLM call. Defaults to 30s.
	Timeout time.Duration

	// MaxTokens caps the response length. Defaults to 512.
	MaxTokens int
}

func (a *LLMQuestioner) ID() string   { return "questioner" }
func (a *LLMQuestioner) Name() string { return "History & Symptom Questioner (LLM)" }
func (a *LLMQuestioner) Capabilities() []string {
	return []string{"ask_question", "history_taking", "llm_backed"}
}

// questionerOutput mirrors the JSON shape the LLM is asked to produce.
type questionerOutput struct {
	Questions  []questionerQA `json:"questions"`
	Hypotheses []string       `json:"hypotheses"`
}

type questionerQA struct {
	Q string `json:"q"`
	A string `json:"a"`
}

func (a *LLMQuestioner) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}
	if state.Answers == nil {
		state.Answers = make(map[string]string)
	}

	out, llmErr := a.callLLM(ctx, state, env)
	if llmErr != nil {
		env.Logf("[questioner/llm] %s — falling back to rule-based", llmErr)
		return a.runFallback(ctx, msg, env)
	}

	for _, qa := range out.Questions {
		env.Logf("[questioner/llm] ask: %s", qa.Q)
		state.QuestionsAsked = append(state.QuestionsAsked, qa.Q)
		state.Answers[qa.Q] = qa.A
	}
	if len(out.Hypotheses) > 0 {
		state.Hypotheses = out.Hypotheses
	}
	env.Logf("[questioner/llm] %d questions answered; hypotheses: %v", len(state.QuestionsAsked), state.Hypotheses)

	emit := agent.NewMessage(
		a.ID(),
		"diagnostic_supervisor",
		agent.RoleAgent,
		"questions_complete",
		medical.EncodeState(state),
		copyMeta(msg.Metadata),
	)
	return []agent.Message{emit}, nil
}

func (a *LLMQuestioner) callLLM(ctx context.Context, state medical.CaseState, env agent.Environment) (questionerOutput, error) {
	if a.Client == nil {
		return questionerOutput{}, errors.New("llm client not configured")
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	maxTokens := a.MaxTokens
	if maxTokens == 0 {
		maxTokens = 512
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stateJSON, _ := json.MarshalIndent(state, "", "  ")
	req := llm.Request{
		System:      questionerSystemPrompt,
		CacheSystem: true,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: "Case state:\n" + string(stateJSON) + "\n\nReturn 3-5 history questions with simulated answers and 3 most likely hypotheses.",
		}},
		MaxTokens:   maxTokens,
		Temperature: 0.2,
		JSONSchema: &llm.JSONSchema{
			Name:        "emit_history_questions",
			Description: "Emit 3-5 context-relevant clinical history questions with simulated answers and the 3 most likely hypotheses.",
			Schema:      json.RawMessage(questionerSchema),
		},
	}

	resp, err := a.Client.Complete(cctx, req)
	if err != nil {
		return questionerOutput{}, fmt.Errorf("llm call: %w", err)
	}

	var out questionerOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return questionerOutput{}, fmt.Errorf("decode JSON: %w", err)
	}
	if len(out.Questions) == 0 {
		return questionerOutput{}, errors.New("empty questions list in LLM output")
	}

	env.Logf("[questioner/llm] provider=%s model=%s tokens in=%d out=%d cached=%d stop=%s",
		a.Client.Provider(), a.Client.Model(), resp.InputTokens, resp.OutputTokens, resp.CachedTokens, resp.StopReason)
	return out, nil
}

func (a *LLMQuestioner) runFallback(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Fallback == nil {
		return nil, errors.New("no fallback configured")
	}
	return a.Fallback.HandleMessage(ctx, msg, env)
}

// questionerSystemPrompt is sent (cached) on every LLM questioner call.
const questionerSystemPrompt = `You are the Questioner agent in a virtual physician panel modelled on Microsoft's MAI-DxO sequential diagnosis architecture.

Your role:
  - Read the structured CaseState (presentation, vitals, condition, RPM observations).
  - Produce 3 to 5 context-relevant clinical history questions a physician would ask before ordering tests.
  - For each question, supply a plausible simulated patient answer consistent with the presentation. This is a research / educational system — the answer is treated as a stand-in for what a real patient might say.
  - Produce the 3 most likely starting hypotheses given the presentation and any current evidence.

Constraints:
  - Questions must be specific, open-ended, and grounded in the supplied CaseState — do not invent labs or test results.
  - Do not provide treatment plans or final diagnoses; that is another agent's job.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`

// questionerSchema is the JSON Schema for the questioner output.
const questionerSchema = `{
  "type": "object",
  "properties": {
    "questions": {
      "type": "array",
      "minItems": 3,
      "maxItems": 5,
      "items": {
        "type": "object",
        "properties": {
          "q": { "type": "string", "description": "The clinical history question." },
          "a": { "type": "string", "description": "A plausible simulated patient answer." }
        },
        "required": ["q", "a"],
        "additionalProperties": false
      },
      "description": "3-5 context-relevant clinical history questions with simulated answers."
    },
    "hypotheses": {
      "type": "array",
      "items": { "type": "string" },
      "description": "The 3 most likely starting hypotheses."
    }
  },
  "required": ["questions", "hypotheses"],
  "additionalProperties": false
}`
