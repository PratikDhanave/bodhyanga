package agents

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// CascadeDiagnostician is a small-then-big LLM diagnostician. It first asks a
// cheap, fast model (SmallTier) for a DiagnosisProposal. If that proposal's
// confidence is at least EscalateAt, the small-tier output is forwarded
// unchanged. Otherwise the same input message is replayed against a larger,
// more capable model (LargeTier) and that result is forwarded instead.
//
// The cascade composes two fully-wired LLMDiagnosticians, so each tier already
// owns its own prompt, RAG store, JSON schema, fallback and timeout. The only
// thing the cascade decides is which tier's emission wins. The agent's ID is
// "diagnostician" — same as the base agents — so the supervisor's routing does
// not need to change.
//
// Fallback-detection heuristic:
//
//	A small-tier failure (LLM error, malformed JSON, low confidence) inside the
//	wrapped LLMDiagnostician triggers its rule-based Fallback, which always
//	emits a confidence at-or-below 0.45 (see DiagnosticianAgent.inferDiagnosis).
//	Those proposals therefore land below the default EscalateAt of 0.6 and
//	naturally escalate. The cascade does not need to inspect log lines: a
//	confidence below the threshold — for any reason, from any source — is the
//	signal to escalate.
type CascadeDiagnostician struct {
	// SmallTier is the cheap-first diagnostician. Required.
	SmallTier *LLMDiagnostician

	// LargeTier is the escalation diagnostician. Required.
	LargeTier *LLMDiagnostician

	// EscalateAt is the confidence below which the small tier's output is
	// discarded and the large tier is invoked. Defaults to 0.6.
	EscalateAt float64

	// Timeout, when non-zero, caps the entire cascade (small + escalation)
	// via context.WithTimeout. Individual tiers also enforce their own
	// per-call timeouts.
	Timeout time.Duration
}

// defaultCascadeEscalateAt is the confidence threshold used when EscalateAt
// is left at its zero value. Anything strictly below this triggers a hop to
// the large tier.
const defaultCascadeEscalateAt = 0.6

// ID returns the shared diagnostician agent ID so the supervisor's routing is
// unchanged.
func (a *CascadeDiagnostician) ID() string { return "diagnostician" }

// Name returns the human-readable agent name.
func (a *CascadeDiagnostician) Name() string { return "Diagnostician (Cascade)" }

// Capabilities advertises the cascade alongside the standard diagnostician
// capabilities, so tooling can detect the small→big composition at runtime.
func (a *CascadeDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis", "llm_backed", "cascade"}
}

// HandleMessage runs the small tier first and, if its proposal's confidence is
// below EscalateAt (or the tier returned no usable proposal), reruns the large
// tier on the same input and forwards that instead.
func (a *CascadeDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.SmallTier == nil || a.LargeTier == nil {
		return nil, errors.New("cascade diagnostician: both tiers must be set")
	}

	// Validate the payload up-front so the failure mode mirrors the base
	// LLMDiagnostician (a malformed CaseState is the caller's bug, not a
	// signal to escalate).
	if _, err := medical.DecodeState(msg.Content); err != nil {
		return nil, err
	}

	cctx := ctx
	if a.Timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, a.Timeout)
		defer cancel()
	}

	threshold := a.EscalateAt
	if threshold <= 0 {
		threshold = defaultCascadeEscalateAt
	}

	smallProvider, smallModel := tierIdentity(a.SmallTier)
	largeProvider, largeModel := tierIdentity(a.LargeTier)

	smallOut, smallErr := a.SmallTier.HandleMessage(cctx, msg, env)
	if smallErr == nil {
		if conf, ok := extractProposalConfidence(smallOut); ok && conf >= threshold {
			env.Logf("[diagnostician/cascade] small-tier %s/%s confidence %.2f >= %.2f — keeping",
				smallProvider, smallModel, conf, threshold)
			return smallOut, nil
		} else if ok {
			env.Logf("[diagnostician/cascade] small-tier %s/%s confidence %.2f < %.2f — escalating to large %s/%s",
				smallProvider, smallModel, conf, threshold, largeProvider, largeModel)
		} else {
			env.Logf("[diagnostician/cascade] small-tier %s/%s emitted no diagnosis_proposal — escalating to large %s/%s",
				smallProvider, smallModel, largeProvider, largeModel)
		}
	} else {
		env.Logf("[diagnostician/cascade] small-tier %s/%s errored (%v) — escalating to large %s/%s",
			smallProvider, smallModel, smallErr, largeProvider, largeModel)
	}

	largeOut, largeErr := a.LargeTier.HandleMessage(cctx, msg, env)
	if largeErr != nil {
		// If the large tier also fails outright, surface whichever output
		// the small tier did manage to produce — that's still better than
		// failing the supervisor's routing.
		if smallErr == nil && len(smallOut) > 0 {
			env.Logf("[diagnostician/cascade] large-tier %s/%s errored (%v) — falling back to small-tier emission",
				largeProvider, largeModel, largeErr)
			return smallOut, nil
		}
		return nil, largeErr
	}
	if conf, ok := extractProposalConfidence(largeOut); ok {
		env.Logf("[diagnostician/cascade] large-tier %s/%s confidence %.2f — emitting", largeProvider, largeModel, conf)
	}
	return largeOut, nil
}

// extractProposalConfidence walks an emitted message slice, finds the first
// diagnosis_proposal, decodes it as a medical.DiagnosisProposal and returns
// its Confidence. Returns ok=false when the slice contains no such message or
// the payload does not parse.
func extractProposalConfidence(msgs []agent.Message) (float64, bool) {
	for _, m := range msgs {
		if m.Type != "diagnosis_proposal" {
			continue
		}
		var p medical.DiagnosisProposal
		if err := json.Unmarshal([]byte(m.Content), &p); err != nil {
			return 0, false
		}
		return p.Confidence, true
	}
	return 0, false
}

// tierIdentity returns the provider/model strings of a tier's underlying LLM
// client for logging. Returns ("unknown", "unknown") when the client is nil
// (e.g. a misconfigured tier).
func tierIdentity(t *LLMDiagnostician) (string, string) {
	if t == nil || t.Client == nil {
		return "unknown", "unknown"
	}
	return t.Client.Provider(), t.Client.Model()
}
