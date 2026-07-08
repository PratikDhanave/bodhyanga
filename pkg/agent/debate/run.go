package debate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// DefaultModel is the default Ollama model for this lesson.
const DefaultModel = "qwen2.5:7b"

// positionResponse is the JSON shape we ask each debater to return.
type positionResponse struct {
	Argument   string  `json:"argument"`
	Confidence float64 `json:"confidence"`
}

// synthesisResponse is the JSON shape we ask the chair to return.
type synthesisResponse struct {
	Synthesis  string  `json:"synthesis"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// positionSchema is the JSON Schema for a debater's structured response.
const positionSchema = `{
  "type": "object",
  "properties": {
    "argument":   {"type": "string"},
    "confidence": {"type": "number", "minimum": 0, "maximum": 1}
  },
  "required": ["argument", "confidence"]
}`

// synthesisSchema is the JSON Schema for the chair's structured response.
const synthesisSchema = `{
  "type": "object",
  "properties": {
    "synthesis":   {"type": "string"},
    "confidence":  {"type": "number", "minimum": 0, "maximum": 1},
    "reasoning":   {"type": "string"}
  },
  "required": ["synthesis", "confidence", "reasoning"]
}`

// chairSystemPrompt is the system prompt for the neutral chair agent.
const chairSystemPrompt = `You are a neutral synthesizer chair facilitating a structured debate. Multiple expert agents have each argued a position on the question. Your job:
  - Read every participant's argument and confidence.
  - Synthesise the best, most balanced answer that draws on the strongest points from each position.
  - Provide a brief reasoning for your synthesis.
  - Calibrate confidence: high when positions converge, lower when they diverge significantly.
Return ONLY JSON matching the supplied schema. No prose outside the schema fields.`

// debaterResult is the per-goroutine return slot for concurrent debater calls.
type debaterResult struct {
	debater  Debater
	position Position
	err      error
}

// RunDebateAgent runs the multi-agent debate. Each debater in the panel is
// called concurrently (goroutines + WaitGroup). After all positions are
// collected, a neutral chair LLM call synthesises the best answer.
//
// callbacks may be nil or have nil fields — hooks that are nil are skipped.
func RunDebateAgent(ctx context.Context, client llm.Client, question string, debaters []Debater, callbacks DebateCallbacks) (DebateResult, error) {
	if len(debaters) == 0 {
		debaters = DefaultDebaters()
	}

	results := make([]debaterResult, len(debaters))
	var wg sync.WaitGroup

	// Step 1: Run each debater concurrently.
	for i := range debaters {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := debaters[idx]
			pos, err := callDebater(ctx, client, question, d)
			results[idx] = debaterResult{debater: d, position: pos, err: err}
		}(i)
	}
	wg.Wait()

	// Step 2: Collect successful positions; accumulate errors.
	positions := make([]Position, 0, len(results))
	var errs []string
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.debater.Name, r.err))
			continue
		}
		positions = append(positions, r.position)
		if callbacks.OnPosition != nil {
			callbacks.OnPosition(r.position.Debater, r.position.Argument)
		}
	}

	// Require at least one successful position before synthesising.
	if len(positions) == 0 {
		return DebateResult{}, fmt.Errorf("debate: all debaters failed: %s", strings.Join(errs, "; "))
	}

	// Step 3: Chair synthesis — present all positions and ask for the best answer.
	synthesis, synthConf, err := callChair(ctx, client, question, positions)
	if err != nil {
		return DebateResult{}, fmt.Errorf("debate: chair synthesis failed: %w", err)
	}

	if callbacks.OnSynthesis != nil {
		callbacks.OnSynthesis(synthesis)
	}

	return DebateResult{
		Question:   question,
		Positions:  positions,
		Synthesis:  synthesis,
		Confidence: synthConf,
	}, nil
}

// callDebater calls the LLM for a single debater, requesting structured JSON.
func callDebater(ctx context.Context, client llm.Client, question string, d Debater) (Position, error) {
	prompt := fmt.Sprintf("Question: %s\n\nArgue your position clearly based on your perspective. Return JSON with your argument and confidence score (0.0-1.0).", question)

	resp, err := client.Complete(ctx, llm.Request{
		System:      d.Perspective,
		CacheSystem: true,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		},
		MaxTokens:   512,
		Temperature: 0.7,
		JSONSchema: &llm.JSONSchema{
			Name:        "emit_position",
			Description: "Emit this debater's argued position with confidence score.",
			Schema:      json.RawMessage(positionSchema),
		},
	})
	if err != nil {
		return Position{}, fmt.Errorf("LLM call failed: %w", err)
	}

	var pr positionResponse
	if err := json.Unmarshal([]byte(resp.Content), &pr); err != nil {
		// Fallback: treat the whole response as the argument.
		return Position{
			Debater:    d.Name,
			Argument:   resp.Content,
			Confidence: 0.5,
		}, nil
	}

	// Clamp confidence to [0,1].
	conf := pr.Confidence
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}

	return Position{
		Debater:    d.Name,
		Argument:   pr.Argument,
		Confidence: conf,
	}, nil
}

// callChair presents all debate positions to the neutral chair LLM and asks
// for a synthesised best answer.
func callChair(ctx context.Context, client llm.Client, question string, positions []Position) (synthesis string, confidence float64, err error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\nDebate positions:\n\n", question)
	for _, p := range positions {
		fmt.Fprintf(&b, "- %s (confidence %.0f%%):\n  %s\n\n", p.Debater, p.Confidence*100, p.Argument)
	}
	b.WriteString("Synthesise the best answer drawing on the strongest arguments above.")

	resp, err := client.Complete(ctx, llm.Request{
		System:      chairSystemPrompt,
		CacheSystem: true,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: b.String()},
		},
		MaxTokens:   768,
		Temperature: 0.2,
		JSONSchema: &llm.JSONSchema{
			Name:        "emit_synthesis",
			Description: "Emit the chair's synthesised best answer with confidence and reasoning.",
			Schema:      json.RawMessage(synthesisSchema),
		},
	})
	if err != nil {
		return "", 0, err
	}

	var sr synthesisResponse
	if err := json.Unmarshal([]byte(resp.Content), &sr); err != nil {
		// Fallback: use raw content as synthesis.
		return resp.Content, 0.5, nil
	}

	// Clamp confidence to [0,1].
	conf := sr.Confidence
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}

	return sr.Synthesis, conf, nil
}
