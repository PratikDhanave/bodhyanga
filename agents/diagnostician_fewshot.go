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
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// FewShotDiagnostician composes an LLMDiagnostician with a second
// knowledge.Store of past clinician-approved cases. Before each LLM call it
// retrieves the top-K most relevant approved cases and injects them into the
// user message as an "Examples:" block. The base diagnostician's RAG
// knowledge store (if any) continues to operate independently — Evidence and
// Examples are layered, not exclusive.
//
// The agent's ID remains "diagnostician" so routing is unchanged.
//
// Behaviour falls back gracefully:
//   - Shots == nil → behaves identically to the base LLMDiagnostician.
//   - Search error → logged; LLM is still called without Examples.
//   - LLM error / bad JSON / low confidence → delegates to Base.runFallback,
//     which fires the rule-based agent.
type FewShotDiagnostician struct {
	// Base is the underlying LLM diagnostician whose prompt construction,
	// RAG wiring, fallback, and verifier emission are reused. Required.
	Base *LLMDiagnostician

	// Shots is the knowledge.Store of past clinician-approved cases. May be
	// nil — in that case the agent behaves exactly like Base.
	Shots knowledge.Store

	// K is the number of past-case examples to inject. Defaults to 3.
	K int

	// Timeout caps the LLM call. Defaults to Base.Timeout, else 30s.
	Timeout time.Duration
}

// fewShotMinScore is the floor a retrieved example must clear before it is
// injected into the prompt. Anything below is treated as irrelevant noise.
const fewShotMinScore = 0.3

func (a *FewShotDiagnostician) ID() string   { return "diagnostician" }
func (a *FewShotDiagnostician) Name() string { return "Diagnostician (LLM + Few-shot)" }
func (a *FewShotDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis", "llm_backed", "few_shot"}
}

// HandleMessage decodes the CaseState, retrieves past approved cases,
// augments the base request with an Examples block, and delegates to the
// base completion / decode path. On any failure it falls back via
// Base.runFallback.
func (a *FewShotDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Base == nil {
		return nil, errors.New("fewshot diagnostician: nil Base")
	}
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	proposal, llmErr := a.callLLM(ctx, state, env)
	if llmErr != nil {
		env.Logf("[diagnostician/fewshot] %s — falling back to rule-based", llmErr)
		return a.Base.runFallback(ctx, msg, env)
	}

	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[diagnostician/fewshot] provider=%s model=%s diagnosis=%q confidence=%.0f%%",
		a.Base.Client.Provider(), a.Base.Client.Model(), proposal.Diagnosis, proposal.Confidence*100)
	env.Logf("[diagnostician/fewshot] rationale: %s", proposal.Rationale)

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

// callLLM runs retrieval, prompt augmentation, and the underlying base
// completion. Returns the decoded proposal or any error encountered.
func (a *FewShotDiagnostician) callLLM(ctx context.Context, state medical.CaseState, env agent.Environment) (medical.DiagnosisProposal, error) {
	if a.Base.Client == nil {
		return medical.DiagnosisProposal{}, errors.New("llm client not configured")
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = a.Base.Timeout
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	minConfidence := a.Base.MinConfidence
	if minConfidence == 0 {
		minConfidence = 0.3
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, retrieved, _ := a.Base.buildRequest(cctx, state, env)

	// Retrieve few-shot examples from the HITL-decision store and prepend
	// them to the user message. Failures are logged but never abort the LLM
	// call — falling through to a no-examples prompt is strictly better than
	// firing the rule-based fallback.
	if a.Shots != nil {
		shots := a.retrieveShots(cctx, state, env)
		if len(shots) > 0 && len(req.Messages) > 0 {
			req.Messages[0].Content = renderShots(shots) + req.Messages[0].Content
		}
	}

	return a.Base.completeAndDecode(cctx, req, retrieved, minConfidence, env)
}

// retrieveShots returns the citations whose score clears fewShotMinScore.
// Errors are logged and swallowed: an empty result is the correct degradation.
func (a *FewShotDiagnostician) retrieveShots(ctx context.Context, state medical.CaseState, env agent.Environment) []knowledge.Citation {
	k := a.K
	if k <= 0 {
		k = 3
	}
	query := buildRetrievalQuery(state)
	if query == "" {
		return nil
	}
	cits, err := a.Shots.Search(ctx, query, k)
	if err != nil {
		env.Logf("[diagnostician/fewshot] shots search failed: %v (continuing without examples)", err)
		return nil
	}
	kept := make([]knowledge.Citation, 0, len(cits))
	for _, c := range cits {
		if c.Score < fewShotMinScore {
			continue
		}
		kept = append(kept, c)
	}
	if dropped := len(cits) - len(kept); dropped > 0 {
		env.Logf("[diagnostician/fewshot] dropped %d shots below score=%.2f", dropped, fewShotMinScore)
	}
	if len(kept) > 0 {
		env.Logf("[diagnostician/fewshot] injected %d past-case examples", len(kept))
	}
	return kept
}

// renderShots formats the retrieved citations as a leading prompt block. The
// header phrasing is asserted by tests — keep it in sync.
func renderShots(shots []knowledge.Citation) string {
	var b strings.Builder
	b.WriteString("Examples (past clinician-approved cases):\n")
	for i, c := range shots {
		fmt.Fprintf(&b, "--- example %d ---\n", i+1)
		if c.Title != "" {
			b.WriteString(c.Title)
			b.WriteString("\n")
		}
		b.WriteString(c.Snippet)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}
