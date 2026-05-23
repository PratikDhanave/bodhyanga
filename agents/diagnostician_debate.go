package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// Specialist is one virtual physician persona in the debate panel. Name is a
// short clinical specialty label (e.g. "Cardiology"); Description is a 1–2
// sentence focus statement prepended to the diagnostician system prompt so
// the LLM frames its proposal from that angle.
type Specialist struct {
	Name        string
	Description string
}

// DebateDiagnostician runs N parallel specialist personas against the same
// CaseState, then asks a "chair" LLM call to pick or synthesise the single
// most likely diagnosis. It reuses the wrapped LLMDiagnostician's prompt
// construction, RAG retrieval, JSON schema, fallback wiring, and citation
// reconciliation rather than duplicating any of it.
//
// The agent's ID remains "diagnostician" so the supervisor's routing is
// unchanged.
type DebateDiagnostician struct {
	// Base is the reused LLMDiagnostician. Its Client, Knowledge, Fallback,
	// Timeout, MinConfidence, and buildRequest are all shared. Required.
	Base *LLMDiagnostician

	// Specialists is the panel. Recommended size 2–6. If empty,
	// DefaultSpecialists() is used.
	Specialists []Specialist

	// ChairPrompt overrides the default chair system prompt. Empty uses
	// debateChairSystemPrompt.
	ChairPrompt string

	// Timeout caps each individual specialist or chair LLM call. Defaults
	// to Base.Timeout, or 30s if both are zero.
	Timeout time.Duration

	// MaxTokens caps the response length for the chair call. Defaults to
	// Base.MaxTokens or 512.
	MaxTokens int
}

// maxConcurrentSpecialists caps the goroutine fan-out so we never hammer
// the upstream provider with more than 4 in-flight requests per case.
const maxConcurrentSpecialists = 4

func (a *DebateDiagnostician) ID() string   { return "diagnostician" }
func (a *DebateDiagnostician) Name() string { return "Diagnostician (LLM Debate)" }
func (a *DebateDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis", "llm_backed", "multi_expert_debate"}
}

// DefaultSpecialists returns the 4-specialist panel used when callers do not
// supply their own roster: Cardiology, Infectious Disease, Pulmonology, and
// Emergency Medicine. The mix is biased toward acute adult presentations
// (chest pain / cough / fever / dyspnea) — the bulk of demo cases.
func DefaultSpecialists() []Specialist {
	return []Specialist{
		{
			Name:        "Cardiology",
			Description: "Cardiologist focused on heart failure, acute coronary syndromes, arrhythmias, and valvular disease. Prioritises cardiac causes of dyspnea, chest pain, and abnormal vitals.",
		},
		{
			Name:        "Infectious Disease",
			Description: "Infectious disease specialist focused on bacterial, viral, and fungal infections including pneumonia, sepsis, UTI, and meningitis. Weighs fever curves, white-count trends, and microbiology.",
		},
		{
			Name:        "Pulmonology",
			Description: "Pulmonologist focused on respiratory failure, COPD, asthma, pulmonary embolism, and interstitial lung disease. Interprets oxygenation, lung exam findings, and chest imaging closely.",
		},
		{
			Name:        "Emergency Medicine",
			Description: "Emergency physician focused on undifferentiated acute presentations, ruling in life threats first (MI, PE, sepsis, stroke, AAA) before chasing less urgent diagnoses.",
		},
	}
}

func (a *DebateDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Base == nil {
		return nil, errors.New("debate diagnostician: nil Base")
	}
	if a.Base.Client == nil {
		env.Logf("[diagnostician/debate] llm client not configured — falling back to rule-based")
		return a.Base.runFallback(ctx, msg, env)
	}

	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	specialists := a.Specialists
	if len(specialists) == 0 {
		specialists = DefaultSpecialists()
	}

	timeout := a.Timeout
	if timeout == 0 {
		timeout = a.Base.Timeout
	}
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	minConfidence := a.Base.MinConfidence
	if minConfidence == 0 {
		minConfidence = 0.3
	}

	// Build the shared base request once. retrieved citations are reused
	// across every specialist and the chair so RAG provenance lines up
	// regardless of which proposal the chair picks.
	baseCtx, baseCancel := context.WithTimeout(ctx, timeout)
	baseReq, retrieved, _ := a.Base.buildRequest(baseCtx, state, env)
	baseCancel()

	results := make([]specialistResult, len(specialists))
	sem := make(chan struct{}, maxConcurrentSpecialists)
	var wg sync.WaitGroup

	for i := range specialists {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			sCtx, sCancel := context.WithTimeout(ctx, timeout)
			defer sCancel()

			req := cloneRequestForSpecialist(baseReq, specialists[idx])
			p, err := a.Base.completeAndDecode(sCtx, req, retrieved, minConfidence, env)
			results[idx] = specialistResult{spec: specialists[idx], proposal: p, err: err}
		}(i)
	}
	wg.Wait()

	// Filter successes; log failures.
	successes := make([]specialistResult, 0, len(results))
	for _, r := range results {
		if r.err != nil {
			env.Logf("[diagnostician/debate] specialist %q failed: %v", r.spec.Name, r.err)
			continue
		}
		successes = append(successes, r)
	}

	env.Logf("[diagnostician/debate] %d/%d specialists succeeded", len(successes), len(results))

	// If fewer than half succeeded, fall back. Integer division — for N=4
	// we require >=2; for N=3 we require >=2 (since 3/2 == 1, and ">"
	// means strictly more than half).
	if len(successes)*2 < len(results) {
		env.Logf("[diagnostician/debate] insufficient specialist quorum — falling back to rule-based")
		return a.Base.runFallback(ctx, msg, env)
	}

	// Chair synthesis.
	chairCtx, chairCancel := context.WithTimeout(ctx, timeout)
	defer chairCancel()

	chairReq := a.buildChairRequest(successes, retrieved)
	final, err := a.Base.completeAndDecode(chairCtx, chairReq, retrieved, minConfidence, env)
	if err != nil {
		env.Logf("[diagnostician/debate] chair synthesis failed: %v — falling back to rule-based", err)
		return a.Base.runFallback(ctx, msg, env)
	}

	state.WorkingDiagnosis = final.Diagnosis
	env.Logf("[diagnostician/debate] provider=%s model=%s diagnosis=%q confidence=%.0f%% specialists=%d",
		a.Base.Client.Provider(), a.Base.Client.Model(), final.Diagnosis, final.Confidence*100, len(successes))
	env.Logf("[diagnostician/debate] rationale: %s", final.Rationale)

	content, _ := json.Marshal(final)
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

// cloneRequestForSpecialist deep-copies the shared base request and prepends
// the specialist's persona prefix to the system prompt. Messages are copied
// by element so the per-specialist requests do not share the underlying
// Images slice with one another.
func cloneRequestForSpecialist(base llm.Request, s Specialist) llm.Request {
	r := base
	prefix := "You are the " + s.Name + " expert. " + s.Description + "\n\n"
	r.System = prefix + base.System
	r.Messages = append([]llm.Message(nil), base.Messages...)
	return r
}

// buildChairRequest builds a fresh llm.Request that lists each surviving
// specialist's name + proposal JSON and asks the chair to pick or synthesise
// the single most likely diagnosis. Uses the same JSON Schema as the base
// diagnostician so the response decodes into a DiagnosisProposal.
func (a *DebateDiagnostician) buildChairRequest(results []specialistResult, retrieved []medical.DiagnosisCitation) llm.Request {
	system := a.ChairPrompt
	if system == "" {
		system = debateChairSystemPrompt
	}

	var b strings.Builder
	b.WriteString("Specialist proposals from the virtual panel:\n\n")
	for _, r := range results {
		propJSON, _ := json.Marshal(r.proposal)
		fmt.Fprintf(&b, "- %s:\n%s\n\n", r.spec.Name, string(propJSON))
	}
	if len(retrieved) > 0 {
		b.WriteString("Evidence already retrieved for this case:\n")
		for _, c := range retrieved {
			fmt.Fprintf(&b, "- [%s] %s — %s\n", c.Source, c.Title, c.Snippet)
		}
		b.WriteString("\n")
	}
	b.WriteString("Return one DiagnosisProposal representing the panel's final working diagnosis.")

	maxTokens := a.MaxTokens
	if maxTokens == 0 {
		maxTokens = a.Base.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = 512
	}

	// Pick the same schema variant the base would have used so the chair's
	// JSON shape matches the citation-or-no-citation contract.
	schema := diagnosisSchema
	if a.Base.Knowledge != nil {
		schema = diagnosisSchemaRAG
	}

	return llm.Request{
		System:      system,
		CacheSystem: true,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: b.String(),
		}},
		MaxTokens:   maxTokens,
		Temperature: 0.2,
		JSONSchema: &llm.JSONSchema{
			Name:        "emit_diagnosis",
			Description: "Emit the panel's single working diagnosis with confidence, differential, rationale, and (when evidence was supplied) citations.",
			Schema:      json.RawMessage(schema),
		},
	}
}

// specialistResult is the per-goroutine return slot. Declared at package
// scope (rather than nested inside HandleMessage) so buildChairRequest can
// take it as a parameter — Go does not let methods accept function-local
// types.
type specialistResult struct {
	spec     Specialist
	proposal medical.DiagnosisProposal
	err      error
}

// debateChairSystemPrompt is the default system prompt for the chair LLM
// call. Cached on every debate.
const debateChairSystemPrompt = `You are the chair of a virtual physician panel modelled on Microsoft's MAI-DxO sequential diagnosis architecture.

Multiple specialists have each proposed a working diagnosis for the same case. Your job:
  - Read every specialist's proposal (diagnosis, confidence, differential, rationale, and any citations).
  - Pick or synthesise the single most likely working diagnosis grounded in the evidence the specialists cite.
  - Combine the strongest differentials into one ordered list of up to five.
  - Write a 1-3 sentence rationale that names the deciding evidence — do not invent labs or findings.
  - Calibrate confidence to the agreement level: high when multiple specialists converge on the same diagnosis, lower when the panel splits.

Evidence grounding:
  - If an "Evidence already retrieved" block is present, you may reuse its references in the citations array.
  - Do not fabricate citations not present in the specialists' proposals or the Evidence block.

Constraints:
  - This is a research / educational system. Do not provide treatment plans.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`
