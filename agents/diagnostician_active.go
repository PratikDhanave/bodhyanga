package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// ActiveLearningDiagnostician composes an LLMDiagnostician with a rule-based
// DiagnosticianAgent so every case is diagnosed twice: once by the LLM path
// (primary) and once by the deterministic rules (comparator). When the two
// answers disagree on diagnosis label, the case is automatically routed into
// the human_review queue as a labelled training-data candidate — the
// clinician's decision becomes a high-signal example that
// cmd/export-hitl-feedback later ships out as training data.
//
// The agent ID remains "diagnostician" so the supervisor's routing does not
// have to change. The LLM proposal is always emitted toward
// reasoning_verifier so the pipeline continues uninterrupted; the
// review_request is purely additive — the existing rule (HITL is optional
// from upstream) keeps the system runnable even with active learning off.
type ActiveLearningDiagnostician struct {
	// LLM is the primary diagnostic path. Required.
	LLM *LLMDiagnostician

	// Rule is the comparator. Defaults to &DiagnosticianAgent{} when nil.
	Rule agent.Agent

	// HITL routes disagreements into the review queue when true. When
	// false, disagreements are logged only — useful for dry-run /
	// shadow-mode rollouts where the team wants to measure disagreement
	// rate before paying clinician review time.
	HITL bool

	// AgreementThreshold is the minimum token-Jaccard similarity between
	// the two normalised diagnosis strings considered "agreement". <= 0
	// defaults to defaultActiveLearningAgreementThreshold (0.6).
	AgreementThreshold float64
}

// defaultActiveLearningAgreementThreshold is the Jaccard cut-off below which
// LLM and rule diagnoses are treated as disagreeing. 0.6 is loose enough that
// surface paraphrases of the same diagnosis still match while real label
// divergence ("Acute pulmonary embolism" vs "Community-acquired pneumonia")
// triggers a review.
const defaultActiveLearningAgreementThreshold = 0.6

// activeLearningReviewKind is the HITL kind label written to the review
// queue. The HITL UI / API uses this string to bucket training-data items
// separately from clinical sign-off reviews.
const activeLearningReviewKind = "active_learning_disagreement"

// activeLearningNextType is the message type used for the resumed payload
// after a clinician decides on an active-learning review. The engagement
// agent currently no-ops on this type — the review's value is the
// clinician's labelled decision, not pipeline resumption.
const activeLearningNextType = "active_learning_logged"

// activeLearningSLAMinutes is the soft deadline (24h) attached to
// disagreement reviews. These are training-data candidates, not urgent
// clinical decisions, so the SLA is intentionally relaxed.
const activeLearningSLAMinutes = 24 * 60

func (a *ActiveLearningDiagnostician) ID() string   { return "diagnostician" }
func (a *ActiveLearningDiagnostician) Name() string { return "Diagnostician (Active Learning)" }
func (a *ActiveLearningDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "active_learning", "llm_backed", "hitl_capable"}
}

func (a *ActiveLearningDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.LLM == nil {
		return nil, errors.New("active-learning diagnostician: nil LLM base")
	}

	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	rule := a.Rule
	if rule == nil {
		rule = &DiagnosticianAgent{}
	}
	threshold := a.AgreementThreshold
	if threshold <= 0 {
		threshold = defaultActiveLearningAgreementThreshold
	}

	// Run the LLM path. Its existing fallback to rule-based engages
	// transparently on LLM error.
	llmOut, err := a.LLM.HandleMessage(ctx, msg, env)
	if err != nil {
		return nil, err
	}
	llmProposal, ok := extractActiveLearningProposal(llmOut)
	if !ok {
		env.Logf("[diagnostician/active-learning] could not decode LLM proposal — emitting LLM output as-is")
		return llmOut, nil
	}

	// Run the rule comparator directly. We re-use the same incoming
	// Message (same Content / Metadata), so the rule agent sees an
	// identical CaseState. This is a synthetic in-process call: we do not
	// publish onto the bus; we invoke HandleMessage directly and decode
	// its emitted diagnosis_proposal.
	ruleOut, err := rule.HandleMessage(ctx, msg, env)
	if err != nil {
		env.Logf("[diagnostician/active-learning] rule comparator failed: %v — emitting LLM output only", err)
		return llmOut, nil
	}
	ruleProposal, ok := extractActiveLearningProposal(ruleOut)
	if !ok {
		env.Logf("[diagnostician/active-learning] could not decode rule proposal — emitting LLM output only")
		return llmOut, nil
	}

	score := tokenJaccard(ruleProposal.Diagnosis, llmProposal.Diagnosis)
	if score >= threshold {
		env.Logf("[diagnostician/active-learning] AGREE llm=%q rule=%q jaccard=%.2f >= %.2f",
			llmProposal.Diagnosis, ruleProposal.Diagnosis, score, threshold)
		return llmOut, nil
	}

	if !a.HITL {
		env.Logf("[diagnostician/active-learning] disagreement (NOT routed — HITL off) llm=%q rule=%q jaccard=%.2f < %.2f",
			llmProposal.Diagnosis, ruleProposal.Diagnosis, score, threshold)
		return llmOut, nil
	}

	env.Logf("[diagnostician/active-learning] disagreement routed to human_review llm=%q rule=%q jaccard=%.2f < %.2f",
		llmProposal.Diagnosis, ruleProposal.Diagnosis, score, threshold)

	review := a.buildReviewMessage(msg, state, llmProposal, ruleProposal, score)
	return append(llmOut, review), nil
}

// extractActiveLearningProposal pulls the first diagnosis_proposal out of an
// emitted message slice. Returns (proposal, true) on success; (zero, false)
// when the slice is empty, the relevant message is missing, or JSON decoding
// fails. Defensive — the agent degrades to emitting only the LLM output
// rather than crashing the pipeline on a misshapen comparator response.
func extractActiveLearningProposal(out []agent.Message) (medical.DiagnosisProposal, bool) {
	for _, m := range out {
		if m.Type != "diagnosis_proposal" {
			continue
		}
		var p medical.DiagnosisProposal
		if err := json.Unmarshal([]byte(m.Content), &p); err != nil {
			continue
		}
		if p.Diagnosis == "" {
			continue
		}
		return p, true
	}
	return medical.DiagnosisProposal{}, false
}

// buildReviewMessage constructs the review_request message routed to
// human_review when the LLM and rule diagnoses disagree. The clinician sees
// both proposals side-by-side in Detail; the Payload carries the structured
// JSON the HITL-feedback exporter will later consume as a labelled training
// example.
func (a *ActiveLearningDiagnostician) buildReviewMessage(
	msg agent.Message,
	state medical.CaseState,
	llmProposal, ruleProposal medical.DiagnosisProposal,
	jaccard float64,
) agent.Message {
	subject := fmt.Sprintf("LLM/rule disagreement: %s vs %s",
		llmProposal.Diagnosis, ruleProposal.Diagnosis)
	detail := formatActiveLearningDetail(llmProposal, ruleProposal, jaccard)

	payloadJSON, _ := json.Marshal(struct {
		LLM        medical.DiagnosisProposal `json:"llm"`
		Rule       medical.DiagnosisProposal `json:"rule"`
		CaseState  medical.CaseState         `json:"case_state"`
		Jaccard    float64                   `json:"jaccard"`
		AgreedAt   float64                   `json:"agreement_threshold"`
	}{
		LLM:       llmProposal,
		Rule:      ruleProposal,
		CaseState: state,
		Jaccard:   jaccard,
		AgreedAt:  a.thresholdEffective(),
	})

	meta := reviewEnvelope(msg.Metadata, reviewOptions{
		Kind:       activeLearningReviewKind,
		NextTo:     "engagement",
		NextType:   activeLearningNextType,
		Priority:   "normal",
		Subject:    subject,
		SLAMinutes: activeLearningSLAMinutes,
	})
	// Detail is the human-readable side-by-side block; the structured
	// payload travels separately so the HITL exporter can read it back
	// without parsing prose.
	meta["active_learning_detail"] = detail
	return agent.NewMessage(
		a.ID(),
		"human_review",
		agent.RoleAgent,
		"review_request",
		string(payloadJSON),
		meta,
	)
}

func (a *ActiveLearningDiagnostician) thresholdEffective() float64 {
	if a.AgreementThreshold <= 0 {
		return defaultActiveLearningAgreementThreshold
	}
	return a.AgreementThreshold
}

// formatActiveLearningDetail produces the multi-line, side-by-side string the
// clinician sees in the HITL queue when an LLM/rule disagreement lands.
func formatActiveLearningDetail(llm, rule medical.DiagnosisProposal, jaccard float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "LLM and rule-based diagnosticians disagree (token-jaccard=%.2f).\n\n", jaccard)
	b.WriteString("LLM proposal:\n")
	fmt.Fprintf(&b, "  Diagnosis : %s\n", llm.Diagnosis)
	fmt.Fprintf(&b, "  Confidence: %.2f\n", llm.Confidence)
	if llm.Rationale != "" {
		fmt.Fprintf(&b, "  Rationale : %s\n", llm.Rationale)
	}
	b.WriteString("\nRule proposal:\n")
	fmt.Fprintf(&b, "  Diagnosis : %s\n", rule.Diagnosis)
	fmt.Fprintf(&b, "  Confidence: %.2f\n", rule.Confidence)
	if rule.Rationale != "" {
		fmt.Fprintf(&b, "  Rationale : %s\n", rule.Rationale)
	}
	return b.String()
}

// tokenJaccard returns the Jaccard token-overlap similarity of a and b after
// lowercasing and splitting on every non-alphanumeric rune. Returns 1.0 when
// both inputs tokenise to the empty set (treat "vacuous == vacuous") and 0.0
// when only one side is empty. Pure, side-effect-free; safe to call from
// goroutines.
func tokenJaccard(a, b string) float64 {
	at := tokenSet(a)
	bt := tokenSet(b)
	if len(at) == 0 && len(bt) == 0 {
		return 1.0
	}
	if len(at) == 0 || len(bt) == 0 {
		return 0.0
	}
	inter := 0
	for tok := range at {
		if _, ok := bt[tok]; ok {
			inter++
		}
	}
	union := len(at) + len(bt) - inter
	if union == 0 {
		return 0.0
	}
	return float64(inter) / float64(union)
}

// tokenSet lowercases s, splits on every non-alphanumeric rune, and returns
// the unique tokens as a set. Empty tokens are dropped. Used by
// tokenJaccard.
func tokenSet(s string) map[string]struct{} {
	s = strings.ToLower(s)
	out := make(map[string]struct{})
	splitter := func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}
	for _, tok := range strings.FieldsFunc(s, splitter) {
		if tok == "" {
			continue
		}
		out[tok] = struct{}{}
	}
	return out
}
