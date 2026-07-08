package selfcorrect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

const (
	// DefaultMaxRounds is the default cap on critique→revise cycles.
	DefaultMaxRounds = 3

	// DefaultScoreThreshold is the minimum quality score at which the agent
	// stops iterating. A score >= threshold means "good enough".
	DefaultScoreThreshold = 0.85

	// answerSystemPrompt is the system prompt used when generating an answer
	// or a revision. Kept concise so models without large context windows do
	// not waste tokens on preamble.
	answerSystemPrompt = `You are a helpful, precise assistant. Answer the task completely and correctly.
When revising a previous answer, address every issue raised in the critique.
Respond with the full revised answer only — no meta-commentary, no apologies.`

	// critiqueSystemPrompt instructs the model to act as a strict reviewer
	// and return a JSON critique object.
	critiqueSystemPrompt = `You are a strict reviewer. Your job is to evaluate the quality of an answer to a task.

Return ONLY a JSON object with this exact shape:
{
  "is_correct": <bool>,
  "issues": [<string>, ...],
  "suggestions": [<string>, ...],
  "score": <float 0.0-1.0>
}

Rules:
- is_correct: true only when the answer is fully correct and needs no changes.
- issues: an array of specific, actionable problems. Empty array when is_correct=true.
- suggestions: one suggestion per issue, same length as issues.
- score: 0.0 = completely wrong, 1.0 = perfect. Be honest and consistent.
- No text outside the JSON object.`
)

// critiqueJSON mirrors the JSON shape the critique LLM call must produce.
type critiqueJSON struct {
	IsCorrect   bool     `json:"is_correct"`
	Issues      []string `json:"issues"`
	Suggestions []string `json:"suggestions"`
	Score       float64  `json:"score"`
}

// RunSelfCorrectAgent runs the reflexion loop: generate → critique → revise.
//
// Parameters:
//   - ctx          — cancellation context; passed to every LLM call.
//   - client       — LLM backend (Ollama, Anthropic, OpenAI, …) from pkg/llm.
//   - task         — the problem or question for the agent to answer.
//   - maxRounds    — maximum number of critique→revise cycles. <= 0 uses
//     DefaultMaxRounds.
//   - scoreThreshold — stop when critique.Score >= threshold. <= 0 or > 1
//     uses DefaultScoreThreshold.
//   - callbacks    — optional lifecycle hooks. All fields may be nil.
//
// Algorithm:
//  1. Generate the initial answer.
//  2. Critique: ask the LLM to review the answer and return structured JSON.
//  3. If score >= threshold OR rounds >= maxRounds → return the result.
//  4. Revise: ask the LLM to fix the answer based on the critique.
//  5. Go to step 2 with the revised answer.
func RunSelfCorrectAgent(
	ctx context.Context,
	client llm.Client,
	task string,
	maxRounds int,
	scoreThreshold float64,
	callbacks SelfCorrectCallbacks,
) (RevisionResult, error) {
	if maxRounds <= 0 {
		maxRounds = DefaultMaxRounds
	}
	if scoreThreshold <= 0 || scoreThreshold > 1 {
		scoreThreshold = DefaultScoreThreshold
	}

	// --- Round 1: generate the initial answer ---
	answer, err := generateAnswer(ctx, client, task, "", nil)
	if err != nil {
		return RevisionResult{}, fmt.Errorf("selfcorrect: initial answer: %w", err)
	}
	if callbacks.OnAnswer != nil {
		callbacks.OnAnswer(1, answer)
	}

	originalAnswer := answer
	var firstScore float64
	var lastCritique CritiqueResult

	for round := 1; round <= maxRounds; round++ {
		// --- Critique the current answer ---
		critique, err := runCritique(ctx, client, task, answer)
		if err != nil {
			// Best-effort: on critique failure keep the current answer and stop.
			break
		}
		lastCritique = critique
		if round == 1 {
			firstScore = critique.Score
		}
		if callbacks.OnCritique != nil {
			callbacks.OnCritique(round, critique)
		}

		// --- Check stopping conditions ---
		if critique.Score >= scoreThreshold || critique.IsCorrect {
			result := RevisionResult{
				OriginalAnswer: originalAnswer,
				Critique:       lastCritique,
				RevisedAnswer:  answer,
				Improved:       answer != originalAnswer && critique.Score > firstScore,
				Rounds:         round,
			}
			if callbacks.OnDone != nil {
				callbacks.OnDone(result)
			}
			return result, nil
		}

		// --- Stop if this was the last allowed round ---
		if round == maxRounds {
			break
		}

		// --- Revise the answer based on the critique ---
		revised, err := generateAnswer(ctx, client, task, answer, &critique)
		if err != nil {
			// Best-effort: on revision failure keep the current answer.
			break
		}
		if callbacks.OnRevision != nil {
			callbacks.OnRevision(round, revised)
		}
		answer = revised
	}

	result := RevisionResult{
		OriginalAnswer: originalAnswer,
		Critique:       lastCritique,
		RevisedAnswer:  answer,
		Improved:       answer != originalAnswer && lastCritique.Score > firstScore,
		Rounds:         maxRounds,
	}
	if callbacks.OnDone != nil {
		callbacks.OnDone(result)
	}
	return result, nil
}

// generateAnswer sends a prompt to the LLM to produce or revise an answer.
// When prevAnswer and critique are both non-empty/nil this is a revision call;
// otherwise it is the initial draft.
func generateAnswer(ctx context.Context, client llm.Client, task, prevAnswer string, critique *CritiqueResult) (string, error) {
	var userPrompt string
	if prevAnswer == "" || critique == nil {
		// Initial answer.
		userPrompt = fmt.Sprintf("Task: %s", task)
	} else {
		// Revision: include the issues and suggestions from the critique.
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Task: %s\n\n", task))
		sb.WriteString("Your previous answer:\n")
		sb.WriteString(prevAnswer)
		sb.WriteString("\n\nThe following issues were found:\n")
		for i, issue := range critique.Issues {
			sb.WriteString(fmt.Sprintf("- Issue: %s\n", issue))
			if i < len(critique.Suggestions) {
				sb.WriteString(fmt.Sprintf("  Fix: %s\n", critique.Suggestions[i]))
			}
		}
		sb.WriteString("\nRevise your answer to fix all of the issues above.")
		userPrompt = sb.String()
	}

	resp, err := client.Complete(ctx, llm.Request{
		System: answerSystemPrompt,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: userPrompt},
		},
		MaxTokens:   2048,
		Temperature: 0.3,
	})
	if err != nil {
		return "", fmt.Errorf("llm answer call: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

// runCritique asks the LLM to evaluate the answer and returns a structured
// CritiqueResult. The LLM is instructed via critiqueSystemPrompt and a
// JSONSchema to return a well-formed object.
func runCritique(ctx context.Context, client llm.Client, task, answer string) (CritiqueResult, error) {
	userPrompt := fmt.Sprintf("Task: %s\n\nAnswer to evaluate:\n%s", task, answer)

	resp, err := client.Complete(ctx, llm.Request{
		System:      critiqueSystemPrompt,
		CacheSystem: true,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: userPrompt},
		},
		MaxTokens:   512,
		Temperature: 0.1,
		JSONSchema: &llm.JSONSchema{
			Name:        "critique_answer",
			Description: "Evaluate the quality of an answer to a task and return structured feedback.",
			Schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "is_correct":   { "type": "boolean" },
    "issues":       { "type": "array", "items": { "type": "string" } },
    "suggestions":  { "type": "array", "items": { "type": "string" } },
    "score":        { "type": "number", "minimum": 0, "maximum": 1 }
  },
  "required": ["is_correct", "issues", "suggestions", "score"],
  "additionalProperties": false
}`),
		},
	})
	if err != nil {
		return CritiqueResult{}, fmt.Errorf("llm critique call: %w", err)
	}

	var cj critiqueJSON
	if err := json.Unmarshal([]byte(resp.Content), &cj); err != nil {
		// Fallback: try to salvage a partial parse. Return a lenient critique
		// so the caller can decide to stop rather than crashing.
		return CritiqueResult{
			IsCorrect:   false,
			Issues:      []string{"critique response was not valid JSON"},
			Suggestions: []string{"retry with a different model"},
			Score:       0,
		}, fmt.Errorf("decode critique JSON: %w", err)
	}

	// Normalise nil slices to empty slices for consistent downstream handling.
	if cj.Issues == nil {
		cj.Issues = []string{}
	}
	if cj.Suggestions == nil {
		cj.Suggestions = []string{}
	}

	// critiqueJSON and CritiqueResult have identical field types; convert
	// directly rather than restating each field.
	return CritiqueResult(cj), nil
}
