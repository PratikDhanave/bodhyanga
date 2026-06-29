package graduation

import "fmt"

// Criteria is the evidence bar an agent must clear to advance ONE stage.
type Criteria struct {
	// MinObservations is the minimum number of scored interactions required
	// before a promotion can even be considered. Guards against promoting on a
	// handful of lucky cases.
	MinObservations int
	// MinWilsonLower is the threshold the Wilson lower bound of the success
	// rate must meet or exceed (e.g. 0.95). Because it is the LOWER bound, this
	// simultaneously demands a high rate and a large enough sample.
	MinWilsonLower float64
	// Z is the confidence z-score. Zero ⇒ DefaultZ (1.96, 95%).
	Z float64
}

// DefaultCriteria returns the autopilot-promotion bar from the decoupling plan:
// a 0.95 Wilson lower bound over at least 100 observations at 95% confidence.
func DefaultCriteria() Criteria {
	return Criteria{MinObservations: 100, MinWilsonLower: 0.95, Z: DefaultZ}
}

// Outcome is the verdict of an Evaluate call.
type Outcome string

const (
	// OutcomePromote means the agent has earned the next stage.
	OutcomePromote Outcome = "promote"
	// OutcomeHold means stay at the current stage (not enough evidence, rate
	// too low, terminal stage, or invalid input).
	OutcomeHold Outcome = "hold"
)

// Decision is the full, auditable result of an evaluation: what was decided,
// the target stage, the computed Wilson lower bound, and a PHI-free reason.
type Decision struct {
	From         Stage   `json:"from"`
	To           Stage   `json:"to"`
	Outcome      Outcome `json:"outcome"`
	WilsonLower  float64 `json:"wilson_lower"`
	Observations int     `json:"observations"`
	Reason       string  `json:"reason"`
}

// Evaluate decides whether an agent at stage `from`, with `successes` out of
// `total` scored observations, has earned promotion one rung under `c`. It is
// pure and deterministic — the same inputs always yield the same Decision — so
// it is safe to call on the hot path and trivial to audit.
//
// It never promotes more than one stage at a time, never promotes past
// autopilot, and holds (rather than errors) on invalid input so a misconfigured
// caller fails safe (stays put) instead of advancing.
func Evaluate(from Stage, successes, total int, c Criteria) Decision {
	z := c.Z
	if z <= 0 {
		z = DefaultZ
	}
	lower := WilsonLowerBound(successes, total, z)
	d := Decision{From: from, To: from, Outcome: OutcomeHold, WilsonLower: lower, Observations: total}

	next, ok := from.Next()
	if !ok {
		if from.Valid() {
			d.Reason = fmt.Sprintf("at terminal stage %q; no further promotion", from)
		} else {
			d.Reason = fmt.Sprintf("invalid stage %q; holding", from)
		}
		return d
	}
	if total < c.MinObservations {
		d.Reason = fmt.Sprintf("insufficient observations: %d < %d required", total, c.MinObservations)
		return d
	}
	if lower < c.MinWilsonLower {
		d.Reason = fmt.Sprintf("wilson lower bound %.4f < %.4f required (rate %d/%d)",
			lower, c.MinWilsonLower, successes, total)
		return d
	}

	d.To = next
	d.Outcome = OutcomePromote
	d.Reason = fmt.Sprintf("wilson lower bound %.4f ≥ %.4f over %d observations",
		lower, c.MinWilsonLower, total)
	return d
}
