package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/knowledge"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// LLMDiagnostician is a drop-in replacement for DiagnosticianAgent that asks
// an LLM for a structured diagnosis. On any failure (timeout, JSON parse,
// transport error, low confidence) it delegates to a rule-based fallback so
// the pipeline always produces a diagnosis_proposal.
//
// The agent's ID is "diagnostician" — same as the rule-based one — so the
// supervisor's routing (which targets that ID) does not have to change.
type LLMDiagnostician struct {
	// Client is the LLM provider. Required.
	Client llm.Client

	// Fallback runs when the LLM call fails or returns junk. Required.
	// Typically &DiagnosticianAgent{}.
	Fallback agent.Agent

	// Timeout caps a single LLM call. Defaults to 30s.
	Timeout time.Duration

	// MaxTokens caps the response length. Defaults to 512.
	MaxTokens int

	// MinConfidence below which the LLM's answer is treated as untrusted
	// and the fallback fires. Defaults to 0.3.
	MinConfidence float64

	// Knowledge, if non-nil, enables Retrieval-Augmented Generation. The
	// store is queried with a summary of the CaseState; the top-K citations
	// are injected into the user message as an Evidence block and surfaced
	// on the resulting DiagnosisProposal.Citations. Nil disables RAG.
	Knowledge knowledge.Store

	// RetrievalK is the number of citations to retrieve when Knowledge is
	// set. Defaults to 4.
	RetrievalK int
}

func (a *LLMDiagnostician) ID() string   { return "diagnostician" }
func (a *LLMDiagnostician) Name() string { return "Diagnostician (LLM)" }
func (a *LLMDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis", "llm_backed"}
}

func (a *LLMDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	proposal, llmErr := a.callLLM(ctx, state, msg, env)
	if llmErr != nil {
		env.Logf("[diagnostician/llm] %s — falling back to rule-based", llmErr)
		return a.runFallback(ctx, msg, env)
	}

	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[diagnostician/llm] provider=%s model=%s diagnosis=%q confidence=%.0f%%",
		a.Client.Provider(), a.Client.Model(), proposal.Diagnosis, proposal.Confidence*100)
	env.Logf("[diagnostician/llm] rationale: %s", proposal.Rationale)

	content, _ := json.Marshal(proposal)
	stateContent := medical.EncodeState(state)
	verify := agent.NewMessage(
		a.ID(),
		"reasoning_verifier",
		agent.RoleAgent,
		"diagnosis_proposal",
		string(content),
		mergeStateMeta(msg.Metadata, stateContent),
	)
	return []agent.Message{verify}, nil
}

// buildRequest constructs the llm.Request for the case. It is exported on the
// struct (lowercase, package-internal) so the vision variant can reuse the
// exact prompt/schema/RAG wiring and only mutate the resulting Request to
// attach an image.
func (a *LLMDiagnostician) buildRequest(ctx context.Context, state medical.CaseState, env agent.Environment) (llm.Request, []medical.DiagnosisCitation, int) {
	maxTokens := a.MaxTokens
	if maxTokens == 0 {
		maxTokens = 512
	}

	stateJSON, _ := json.MarshalIndent(state, "", "  ")
	userContent := "Case state:\n" + string(stateJSON) + "\n\nReturn one DiagnosisProposal."

	var retrieved []medical.DiagnosisCitation
	if a.Knowledge != nil {
		k := a.RetrievalK
		if k <= 0 {
			k = 4
		}
		query := buildRetrievalQuery(state)
		if query != "" {
			cits, err := a.Knowledge.Search(ctx, query, k)
			if err != nil {
				env.Logf("[diagnostician/llm] rag search failed: %v (continuing without citations)", err)
			} else if len(cits) > 0 {
				retrieved = make([]medical.DiagnosisCitation, 0, len(cits))
				var b strings.Builder
				b.WriteString("Evidence:\n")
				for _, c := range cits {
					fmt.Fprintf(&b, "- [%s] %s — %s\n", c.Source, c.Title, c.Snippet)
					retrieved = append(retrieved, medical.DiagnosisCitation{
						Source:  c.Source,
						Title:   c.Title,
						Snippet: c.Snippet,
						Score:   c.Score,
					})
				}
				b.WriteString("\n")
				userContent = b.String() + userContent
				env.Logf("[diagnostician/llm] rag retrieved %d citations", len(retrieved))
			}
		}
	}

	system := diagnosticianSystemPrompt
	schema := diagnosisSchema
	if a.Knowledge != nil {
		system = diagnosticianSystemPromptRAG
		schema = diagnosisSchemaRAG
	}

	req := llm.Request{
		System:      system,
		CacheSystem: true,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: userContent,
		}},
		MaxTokens:   maxTokens,
		Temperature: 0.2,
		JSONSchema: &llm.JSONSchema{
			Name:        "emit_diagnosis",
			Description: "Emit a single working diagnosis with confidence, differential, rationale, and (when evidence supplied) citations.",
			Schema:      json.RawMessage(schema),
		},
	}
	return req, retrieved, maxTokens
}

func (a *LLMDiagnostician) callLLM(ctx context.Context, state medical.CaseState, msg agent.Message, env agent.Environment) (medical.DiagnosisProposal, error) {
	if a.Client == nil {
		return medical.DiagnosisProposal{}, errors.New("llm client not configured")
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	minConfidence := a.MinConfidence
	if minConfidence == 0 {
		minConfidence = 0.3
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, retrieved, _ := a.buildRequest(cctx, state, env)
	_ = msg // reserved for future per-call hooks
	return a.completeAndDecode(cctx, req, retrieved, minConfidence, env)
}

// completeAndDecode sends the prepared Request, decodes the proposal, and
// reconciles citations. Split out so the vision variant can reuse it after
// attaching an image to req.
func (a *LLMDiagnostician) completeAndDecode(ctx context.Context, req llm.Request, retrieved []medical.DiagnosisCitation, minConfidence float64, env agent.Environment) (medical.DiagnosisProposal, error) {
	resp, err := a.Client.Complete(ctx, req)
	if err != nil {
		return medical.DiagnosisProposal{}, fmt.Errorf("llm call: %w", err)
	}

	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(resp.Content), &p); err != nil {
		return medical.DiagnosisProposal{}, fmt.Errorf("decode JSON: %w", err)
	}
	if p.Diagnosis == "" {
		return medical.DiagnosisProposal{}, errors.New("empty diagnosis in LLM output")
	}
	if p.Confidence < minConfidence {
		return medical.DiagnosisProposal{}, fmt.Errorf("confidence %.2f below threshold %.2f", p.Confidence, minConfidence)
	}

	// Reconcile citations: if the model surfaced its own, keep them; otherwise
	// propagate the retrieved citations so provenance is preserved.
	if len(p.Citations) == 0 && len(retrieved) > 0 {
		p.Citations = retrieved
	}

	env.Logf("[diagnostician/llm] tokens in=%d out=%d cached=%d stop=%s",
		resp.InputTokens, resp.OutputTokens, resp.CachedTokens, resp.StopReason)
	return p, nil
}

func (a *LLMDiagnostician) runFallback(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Fallback == nil {
		return nil, errors.New("no fallback configured")
	}
	return a.Fallback.HandleMessage(ctx, msg, env)
}

// buildRetrievalQuery composes a single string from the salient fields of the
// CaseState so the embedder gets a query that surfaces guideline / textbook
// matches. We deliberately include presentation, condition tag, a summary of
// test results (key:value pairs joined), and the running hypothesis list.
func buildRetrievalQuery(state medical.CaseState) string {
	parts := make([]string, 0, 4)
	if state.Presentation != "" {
		parts = append(parts, state.Presentation)
	}
	if state.Condition != "" {
		parts = append(parts, state.Condition)
	}
	if len(state.TestResults) > 0 {
		bits := make([]string, 0, len(state.TestResults))
		for name, result := range state.TestResults {
			bits = append(bits, name+": "+result)
		}
		parts = append(parts, strings.Join(bits, "; "))
	}
	if len(state.Hypotheses) > 0 {
		parts = append(parts, strings.Join(state.Hypotheses, ", "))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// diagnosticianSystemPrompt is sent (cached) on every LLM diagnostician call
// when RAG is disabled.
const diagnosticianSystemPrompt = `You are the Diagnostician agent in a virtual physician panel modelled on Microsoft's MAI-DxO sequential diagnosis architecture.

Your role:
  - Read the structured CaseState (presentation, vitals, history answers, ordered tests with results, hypotheses, RPM observations).
  - Propose the single most likely working diagnosis.
  - Provide a 1-3 sentence clinical rationale grounded only in evidence already in the CaseState — do not invent labs, vitals, or history that is not present.
  - Provide a confidence score between 0.0 and 1.0.
  - List up to five differential diagnoses, ordered by likelihood.

Constraints:
  - This is a research / educational system. Do not provide treatment plans.
  - If the evidence is insufficient for a confident diagnosis, return a working label such as "Undifferentiated <complaint> — further workup needed" with confidence below 0.5.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`

// diagnosticianSystemPromptRAG extends the base prompt to instruct the model
// to ground its rationale in the retrieved Evidence block and to emit a
// matching citations array.
const diagnosticianSystemPromptRAG = `You are the Diagnostician agent in a virtual physician panel modelled on Microsoft's MAI-DxO sequential diagnosis architecture.

Your role:
  - Read the structured CaseState (presentation, vitals, history answers, ordered tests with results, hypotheses, RPM observations).
  - Propose the single most likely working diagnosis.
  - Provide a 1-3 sentence clinical rationale grounded only in evidence already in the CaseState — do not invent labs, vitals, or history that is not present.
  - Provide a confidence score between 0.0 and 1.0.
  - List up to five differential diagnoses, ordered by likelihood.

Evidence grounding:
  - The user message may begin with an "Evidence:" block listing retrieved clinical references in the form "- [source] title — snippet".
  - When Evidence is supplied, ground your rationale in those references and populate the "citations" array with the {source, title, snippet, score} of every reference you actually used. Do not fabricate citations not present in Evidence.

Constraints:
  - This is a research / educational system. Do not provide treatment plans.
  - If the evidence is insufficient for a confident diagnosis, return a working label such as "Undifferentiated <complaint> — further workup needed" with confidence below 0.5.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`

// diagnosisSchema is the JSON Schema for DiagnosisProposal without citations.
const diagnosisSchema = `{
  "type": "object",
  "properties": {
    "diagnosis": {
      "type": "string",
      "description": "The single most likely working diagnosis."
    },
    "confidence": {
      "type": "number",
      "minimum": 0,
      "maximum": 1,
      "description": "Confidence in the working diagnosis from 0.0 to 1.0."
    },
    "differential": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Up to five differential diagnoses ordered by likelihood."
    },
    "rationale": {
      "type": "string",
      "description": "1-3 sentence clinical rationale grounded in the supplied CaseState."
    }
  },
  "required": ["diagnosis", "confidence", "rationale"],
  "additionalProperties": false
}`

// diagnosisSchemaRAG is the JSON Schema for DiagnosisProposal with optional
// citations. Used when Knowledge retrieval is enabled.
const diagnosisSchemaRAG = `{
  "type": "object",
  "properties": {
    "diagnosis": {
      "type": "string",
      "description": "The single most likely working diagnosis."
    },
    "confidence": {
      "type": "number",
      "minimum": 0,
      "maximum": 1,
      "description": "Confidence in the working diagnosis from 0.0 to 1.0."
    },
    "differential": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Up to five differential diagnoses ordered by likelihood."
    },
    "rationale": {
      "type": "string",
      "description": "1-3 sentence clinical rationale grounded in the supplied CaseState and Evidence."
    },
    "citations": {
      "type": "array",
      "description": "Retrieved references the rationale relies on. Subset of the supplied Evidence block.",
      "items": {
        "type": "object",
        "properties": {
          "source":  { "type": "string" },
          "title":   { "type": "string" },
          "snippet": { "type": "string" },
          "score":   { "type": "number" }
        },
        "required": ["source", "title"],
        "additionalProperties": false
      }
    }
  },
  "required": ["diagnosis", "confidence", "rationale"],
  "additionalProperties": false
}`
