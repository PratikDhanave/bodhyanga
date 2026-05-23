package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// DifferentialRanker re-ranks the Differential list of a DiagnosisProposal
// using the LLM. The primary Diagnosis is NOT changed — only the order
// (and possibly the set, if the LLM adds or drops entries) of the
// Differential list. On any LLM failure, the original differential is
// returned unchanged. Compose, don't modify: callers run a regular
// diagnostician first, then optionally call Rerank on the proposal.
type DifferentialRanker struct {
	// Client is the LLM provider. Required.
	Client llm.Client

	// Timeout caps a single LLM call. Defaults to 30s.
	Timeout time.Duration

	// MaxTokens caps the response length. Defaults to 256 — the
	// schema only allows up to 10 short diagnosis strings.
	MaxTokens int
}

// differentialRankerSystemPrompt is sent cached on every call.
const differentialRankerSystemPrompt = `You are a clinical reasoning aid. Given a working diagnosis and a case summary, re-rank the differential diagnoses from most likely to least likely. Return a JSON array of strings; do not add explanations.`

// differentialRankerSchema constrains the output to a string array of at
// most 10 entries. The provider adapters route this through tool-use /
// response_format so the model can't return prose.
var differentialRankerSchema = json.RawMessage(`{
  "type": "array",
  "items": {"type": "string"},
  "maxItems": 10
}`)

// errEmptyRankerOutput is an internal sentinel used to fall back to the
// caller's original differential without bothering them with an error.
var errEmptyRankerOutput = errors.New("agents: differential ranker returned empty list")

// Rerank asks the LLM to re-order the supplied proposal's Differential
// list. Returns the new ordering on success; on any failure (LLM
// transport, JSON parse, or empty output) returns the original
// p.Differential unchanged and a nil error — the caller never has to
// branch on errors to keep the pipeline running.
//
// Rerank does NOT mutate the passed proposal. The caller is responsible
// for assigning the returned slice if they want the change to stick.
func (r *DifferentialRanker) Rerank(ctx context.Context, state medical.CaseState, p medical.DiagnosisProposal) ([]string, error) {
	original := append([]string(nil), p.Differential...)
	if r == nil || r.Client == nil {
		return original, nil
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	maxTokens := r.MaxTokens
	if maxTokens == 0 {
		maxTokens = 256
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stateJSON, _ := json.MarshalIndent(state, "", "  ")
	userContent := fmt.Sprintf(
		"Case state:\n%s\n\nWorking diagnosis: %s\n\nCurrent differential (JSON):\n%s\n\nReturn a JSON array of strings ranked most likely first. No explanations.",
		string(stateJSON),
		p.Diagnosis,
		mustJSON(p.Differential),
	)

	resp, err := r.Client.Complete(cctx, llm.Request{
		System:      differentialRankerSystemPrompt,
		CacheSystem: true,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: userContent,
		}},
		MaxTokens:   maxTokens,
		Temperature: 0.0,
		JSONSchema: &llm.JSONSchema{
			Name:        "rerank_differential",
			Description: "Re-ranked differential diagnosis list, most likely first.",
			Schema:      differentialRankerSchema,
		},
	})
	if err != nil {
		return original, nil
	}

	ranked, perr := parseRankedList(resp.Content)
	if perr != nil || len(ranked) == 0 {
		return original, nil
	}
	return ranked, nil
}

// parseRankedList accepts either a bare JSON array of strings or a
// single-key object containing one (some providers wrap arrays for
// schema reasons). Returns the trimmed, non-empty entries.
func parseRankedList(content string) ([]string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, errEmptyRankerOutput
	}
	// Bare array path.
	if strings.HasPrefix(trimmed, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
			return nil, err
		}
		return cleanList(arr), nil
	}
	// Object wrapper path — pick the first string-array field.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return nil, err
	}
	for _, raw := range obj {
		var arr []string
		if err := json.Unmarshal(raw, &arr); err == nil {
			return cleanList(arr), nil
		}
	}
	return nil, errEmptyRankerOutput
}

// cleanList strips empty / whitespace-only entries while preserving
// case-sensitive order (so "CAP" and "Cap" are kept as distinct
// entries the way the LLM emitted them).
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

// mustJSON is a small helper that returns "[]" on marshal failure so the
// prompt always renders cleanly even with weird input.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
