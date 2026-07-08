package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// JudgeSystemPrompt is the cached system prompt that defines the clinical
// grading rubric. It is intentionally a const so reviewers can audit the
// rubric without grepping through builder code.
const JudgeSystemPrompt = `You are an experienced attending physician acting as an impartial grader for
clinical diagnostic reasoning. You will be shown an "expected" gold-standard
answer and an "actual" output from a diagnostician system. Grade the actual
output against the expected one using the following rubric, returning JSON
that conforms exactly to the provided schema.

Rubric (each dimension scored 0.0 - 1.0):

- correctness: Does the working diagnosis match the gold standard, or a
  clinically equivalent label? Synonymous abbreviations and alternative names
  for the same condition count as a match (e.g. "ADHF" is equivalent to
  "acute decompensated heart failure"; "MI" to "myocardial infarction").

- specificity: Is the diagnosis appropriately specific for clinical action?
  Vague catch-alls such as "respiratory infection", "abdominal pain" or
  "viral illness" score low; named conditions with anatomy/etiology score
  high.

- safety: Does the rationale stay within what the evidence supports? Penalize
  claims unsupported by the case, missed red flags, and statements that
  could lead to harm if acted on.

- differential_quality: Did the output include a reasonable differential
  diagnosis covering the most plausible alternatives? An empty or trivial
  differential scores low.

Return a JSON object with these fields:
  score              - overall 0.0..1.0; usually the mean of the dimensional scores
  pass               - true when score >= 0.6
  rationale          - 1-3 sentence justification grounded in the rubric
  dimensional_scores - object with correctness, specificity, safety,
                       differential_quality, each 0.0..1.0

Be terse, be fair, and never reveal chain-of-thought beyond the rationale.`

// Judge grades a candidate output against a reference using an LLM.
type Judge interface {
	Grade(ctx context.Context, item GradeItem) (*Grade, error)
}

// GradeItem is one thing to grade.
type GradeItem struct {
	Scenario string // human label, e.g. "demo-case-001"
	Expected string // gold-standard answer
	Actual   string // what the system produced
	Rubric   string // optional override of the default clinical rubric
	Context  string // optional surrounding context the judge can use
}

// Grade is the judge's verdict.
type Grade struct {
	Score             float64            `json:"score"` // 0.0 - 1.0
	Pass              bool               `json:"pass"`  // score >= 0.6
	Rationale         string             `json:"rationale"`
	DimensionalScores map[string]float64 `json:"dimensional_scores,omitempty"`
	Provider          string             `json:"provider"`
	Model             string             `json:"model"`
}

// PassThreshold is the minimum overall score for Pass to be true.
const PassThreshold = 0.6

// judgeSchema is the JSON Schema returned to the provider; declared as a
// package-level var so we only build the raw message once.
var judgeSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "score": {"type": "number"},
    "pass": {"type": "boolean"},
    "rationale": {"type": "string"},
    "dimensional_scores": {
      "type": "object",
      "properties": {
        "correctness": {"type": "number"},
        "specificity": {"type": "number"},
        "safety": {"type": "number"},
        "differential_quality": {"type": "number"}
      },
      "required": ["correctness", "specificity", "safety", "differential_quality"]
    }
  },
  "required": ["score", "pass", "rationale", "dimensional_scores"]
}`)

// llmJudge implements Judge against a generic llm.Client.
type llmJudge struct {
	client llm.Client
}

// NewLLMJudge returns a Judge backed by the supplied llm.Client.
func NewLLMJudge(client llm.Client) Judge {
	return &llmJudge{client: client}
}

// Grade implements Judge.
func (j *llmJudge) Grade(ctx context.Context, item GradeItem) (*Grade, error) {
	if j.client == nil {
		return nil, errors.New("eval: judge has no llm client")
	}
	system := JudgeSystemPrompt
	if strings.TrimSpace(item.Rubric) != "" {
		system = item.Rubric
	}

	userPrompt := buildJudgePrompt(item)

	resp, err := j.client.Complete(ctx, llm.Request{
		System: system,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: userPrompt},
		},
		MaxTokens:   600,
		Temperature: 0.0,
		CacheSystem: true,
		JSONSchema: &llm.JSONSchema{
			Name:        "clinical_grade",
			Description: "Structured clinical grading verdict.",
			Schema:      judgeSchema,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("eval: judge llm call failed: %w", err)
	}

	var raw struct {
		Score             float64            `json:"score"`
		Pass              *bool              `json:"pass,omitempty"`
		Rationale         string             `json:"rationale"`
		DimensionalScores map[string]float64 `json:"dimensional_scores,omitempty"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("eval: judge returned invalid JSON: %w", err)
	}

	// Clamp every dimensional score to [0,1] so downstream math is safe.
	for k, v := range raw.DimensionalScores {
		raw.DimensionalScores[k] = clamp01(v)
	}
	score := clamp01(raw.Score)

	// Pass is derived from score so we never let the model contradict the
	// numeric threshold (Pass field on the wire is informational).
	g := &Grade{
		Score:             score,
		Pass:              score >= PassThreshold,
		Rationale:         raw.Rationale,
		DimensionalScores: raw.DimensionalScores,
		Provider:          resp.Provider,
		Model:             resp.Model,
	}
	if g.Provider == "" {
		g.Provider = j.client.Provider()
	}
	if g.Model == "" {
		g.Model = j.client.Model()
	}
	return g, nil
}

func buildJudgePrompt(item GradeItem) string {
	var b strings.Builder
	if item.Scenario != "" {
		fmt.Fprintf(&b, "Scenario: %s\n\n", item.Scenario)
	}
	fmt.Fprintf(&b, "Expected (gold standard):\n%s\n\n", strings.TrimSpace(item.Expected))
	fmt.Fprintf(&b, "Actual (diagnostician output):\n%s\n", strings.TrimSpace(item.Actual))
	if strings.TrimSpace(item.Context) != "" {
		fmt.Fprintf(&b, "\nAdditional context:\n%s\n", strings.TrimSpace(item.Context))
	}
	b.WriteString("\nReturn JSON matching the schema. Do not include any prose outside the JSON.")
	return b.String()
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// BatchGrade runs Grade over the supplied items concurrently with a fixed
// pool of 4 workers. The returned slices are aligned with items by index;
// errs[i] is non-nil when grades[i] is nil.
func BatchGrade(ctx context.Context, judge Judge, items []GradeItem) ([]*Grade, []error) {
	grades := make([]*Grade, len(items))
	errs := make([]error, len(items))
	if len(items) == 0 {
		return grades, errs
	}

	const workers = 4
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				g, err := judge.Grade(ctx, items[i])
				grades[i] = g
				errs[i] = err
			}
		}()
	}
	for i := range items {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return grades, errs
}
