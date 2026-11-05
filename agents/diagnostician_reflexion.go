package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// ReflexionDiagnostician wraps a base LLMDiagnostician with a
// Reflexion / Chain-of-Verification (CoVe) loop: after producing the initial
// DiagnosisProposal it asks the same LLM to critique its own answer by
// generating verification questions, answering them from the case evidence,
// and deciding if the diagnosis should stand or be revised. If the critique
// rejects the diagnosis and supplies a revised proposal, the revised one is
// adopted (and may itself be critiqued in further rounds up to MaxRounds).
//
// The agent's ID is "diagnostician" — same as the base — so supervisor
// routing is unchanged.
//
// The base diagnostician is reused as-is for:
//   - the initial draft (via Base.buildRequest + Base.completeAndDecode)
//   - rule-based fallback when the initial draft fails
//   - RAG citations (retrieved once on the initial draft, threaded into the
//     critique prompt as Evidence so the verifier reasons against the same
//     references)
type ReflexionDiagnostician struct {
	// Base is reused for the initial diagnosis, fallback, RAG, and as the
	// llm.Client for critique rounds. Required.
	Base *LLMDiagnostician

	// MaxRounds caps the number of critique→revise rounds. Defaults to 1,
	// clamped to [1, 3].
	MaxRounds int

	// Timeout caps a single critique LLM call. Defaults to 30s.
	Timeout time.Duration

	// MaxTokens caps the critique response length. Defaults to 768
	// (critiques include verification_qa + an optional revised_proposal,
	// which is larger than a plain diagnosis).
	MaxTokens int
}

func (a *ReflexionDiagnostician) ID() string   { return "diagnostician" }
func (a *ReflexionDiagnostician) Name() string { return "Diagnostician (LLM + Reflexion)" }
func (a *ReflexionDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis", "llm_backed", "self_critique"}
}

// critiqueOutput mirrors the JSON shape the critique LLM call produces.
// RevisedProposal is a pointer so "null" round-trips as a nil pointer and we
// can distinguish "unverified but has nothing better to offer" from
// "unverified, here is a better one".
type critiqueOutput struct {
	Verified        bool                         `json:"verified"`
	Reason          string                       `json:"reason"`
	VerificationQA  []critiqueQA                 `json:"verification_qa,omitempty"`
	RevisedProposal *medical.DiagnosisProposal   `json:"revised_proposal,omitempty"`
}

type critiqueQA struct {
	Q string `json:"q"`
	A string `json:"a"`
}

func (a *ReflexionDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Base == nil {
		return nil, errors.New("reflexion diagnostician: nil Base")
	}

	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	// 1. Initial draft using the base diagnostician's exact prompt/RAG wiring.
	proposal, retrieved, llmErr := a.initialDraft(ctx, state, env)
	if llmErr != nil {
		env.Logf("[diagnostician/reflexion] initial draft failed: %s — falling back to rule-based", llmErr)
		return a.Base.runFallback(ctx, msg, env)
	}

	// 2. Reflexion rounds. We deliberately keep the original `retrieved` set
	//    across rounds so every critique sees the same evidence the model
	//    drafted against — re-running retrieval per round would introduce
	//    drift unrelated to the critique signal.
	maxRounds := a.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 1
	}
	if maxRounds > 3 {
		maxRounds = 3
	}

	for round := 1; round <= maxRounds; round++ {
		out, critErr := a.runCritique(ctx, state, proposal, retrieved, round, env)
		if critErr != nil {
			env.Logf("[diagnostician/reflexion] round %d critique failed: %s — keeping current proposal", round, critErr)
			break
		}
		if out.Verified {
			env.Logf("[diagnostician/reflexion] round %d VERIFIED: %s", round, out.Reason)
			break
		}
		if out.RevisedProposal == nil || out.RevisedProposal.Diagnosis == "" {
			env.Logf("[diagnostician/reflexion] round %d UNVERIFIED but no revision supplied (%s) — keeping current proposal", round, out.Reason)
			break
		}
		// Adopt the revision. Preserve original retrieval citations when the
		// revision omits them, mirroring base completeAndDecode behaviour.
		revised := *out.RevisedProposal
		if len(revised.Citations) == 0 && len(retrieved) > 0 {
			revised.Citations = retrieved
		}
		env.Logf("[diagnostician/reflexion] round %d REVISED: %q (was %q) — %s",
			round, revised.Diagnosis, proposal.Diagnosis, out.Reason)
		proposal = revised
	}

	// 3. Emit the final proposal to the reasoning verifier — same envelope
	//    the base diagnostician uses.
	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[diagnostician/reflexion] provider=%s model=%s diagnosis=%q confidence=%.0f%%",
		a.Base.Client.Provider(), a.Base.Client.Model(), proposal.Diagnosis, proposal.Confidence*100)
	env.Logf("[diagnostician/reflexion] rationale: %s", proposal.Rationale)

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

// initialDraft runs the base diagnostician's request once and returns both
// the proposal and the retrieved citations so the critique can reuse them.
func (a *ReflexionDiagnostician) initialDraft(ctx context.Context, state medical.CaseState, env agent.Environment) (medical.DiagnosisProposal, []medical.DiagnosisCitation, error) {
	if a.Base.Client == nil {
		return medical.DiagnosisProposal{}, nil, errors.New("llm client not configured")
	}
	timeout := a.Base.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	minConfidence := a.Base.MinConfidence
	if minConfidence == 0 {
		minConfidence = 0.3
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, retrieved, _ := a.Base.buildRequest(cctx, state, env)
	p, err := a.Base.completeAndDecode(cctx, req, retrieved, minConfidence, env)
	if err != nil {
		return medical.DiagnosisProposal{}, nil, err
	}
	return p, retrieved, nil
}

// runCritique sends a single critique request and decodes the response.
// Errors are returned so the caller can decide to keep the current proposal
// (best effort) rather than aborting the entire diagnosis.
func (a *ReflexionDiagnostician) runCritique(ctx context.Context, state medical.CaseState, current medical.DiagnosisProposal, retrieved []medical.DiagnosisCitation, round int, env agent.Environment) (critiqueOutput, error) {
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	maxTokens := a.MaxTokens
	if maxTokens == 0 {
		maxTokens = 768
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stateJSON, _ := json.MarshalIndent(state, "", "  ")
	proposalJSON, _ := json.MarshalIndent(current, "", "  ")

	var b strings.Builder
	if len(retrieved) > 0 {
		b.WriteString("Evidence:\n")
		for _, c := range retrieved {
			fmt.Fprintf(&b, "- [%s] %s — %s\n", c.Source, c.Title, c.Snippet)
		}
		b.WriteString("\n")
	}
	b.WriteString("CaseState:\n")
	b.Write(stateJSON)
	b.WriteString("\n\nCurrent DiagnosisProposal:\n")
	b.Write(proposalJSON)
	b.WriteString("\n\nCritique this diagnosis. List verification questions, answer each from the supplied evidence, then decide whether the diagnosis stands or needs revision.")

	req := llm.Request{
		System:      reflexionCritiqueSystemPrompt,
		CacheSystem: true,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: b.String(),
		}},
		MaxTokens:   maxTokens,
		Temperature: 0.1,
		JSONSchema: &llm.JSONSchema{
			Name:        "critique_diagnosis",
			Description: "Verify a DiagnosisProposal against the supplied CaseState evidence. Output verified=true to keep it, or false with a revised_proposal to replace it.",
			Schema:      json.RawMessage(reflexionCritiqueSchema),
		},
	}

	resp, err := a.Base.Client.Complete(cctx, req)
	if err != nil {
		return critiqueOutput{}, fmt.Errorf("llm call: %w", err)
	}

	var out critiqueOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return critiqueOutput{}, fmt.Errorf("decode JSON: %w", err)
	}

	env.Logf("[diagnostician/reflexion] round %d tokens in=%d out=%d cached=%d stop=%s",
		round, resp.InputTokens, resp.OutputTokens, resp.CachedTokens, resp.StopReason)
	return out, nil
}

// reflexionCritiqueSystemPrompt is sent (cached) on every critique call.
const reflexionCritiqueSystemPrompt = `You are a clinical reasoning verifier in a virtual physician panel modelled on Microsoft's MAI-DxO sequential diagnosis architecture.

Your role:
  - Critique the supplied DiagnosisProposal against the accompanying CaseState (and the optional Evidence block of retrieved references).
  - Generate 2-5 verification questions of the form: "What evidence supports the diagnosis?", "Are red flags or alternative diagnoses missing?", "Is the stated confidence justified?", "Do any findings contradict the rationale?".
  - Answer each question grounded only in the supplied evidence — do not invent labs, vitals, or history that is not present.
  - Decide whether the proposal stands (verified=true) or needs revision (verified=false).
  - When verified=false, output a revised DiagnosisProposal (diagnosis, confidence, differential, rationale, and citations subset when Evidence was supplied) that addresses the verification answers. When the evidence does not support any concrete revision, set revised_proposal to null.

Constraints:
  - This is a research / educational system. Do not propose treatment plans.
  - Do not fabricate citations beyond the Evidence block.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`

// reflexionCritiqueSchema is the JSON Schema for the critique output. The
// revised_proposal field mirrors diagnosisSchemaRAG (so we can carry citations
// when RAG is enabled) but is nullable so a critique can decline to revise.
const reflexionCritiqueSchema = `{
  "type": "object",
  "properties": {
    "verified": {
      "type": "boolean",
      "description": "True when the diagnosis is grounded in the CaseState and the confidence is not over-stated."
    },
    "reason": {
      "type": "string",
      "description": "One-sentence justification of the verdict."
    },
    "verification_qa": {
      "type": "array",
      "description": "Verification questions and their evidence-grounded answers.",
      "items": {
        "type": "object",
        "properties": {
          "q": { "type": "string" },
          "a": { "type": "string" }
        },
        "required": ["q", "a"],
        "additionalProperties": false
      }
    },
    "revised_proposal": {
      "type": ["object", "null"],
      "description": "Revised DiagnosisProposal when verified=false and a better answer is supported by the evidence. Null when no revision is warranted.",
      "properties": {
        "diagnosis":   { "type": "string" },
        "confidence":  { "type": "number", "minimum": 0, "maximum": 1 },
        "differential": {
          "type": "array",
          "items": { "type": "string" }
        },
        "rationale":   { "type": "string" },
        "citations": {
          "type": "array",
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
      }
    }
  },
  "required": ["verified", "reason"],
  "additionalProperties": false
}`
