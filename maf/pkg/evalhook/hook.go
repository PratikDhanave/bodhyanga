// Package evalhook writes ONE eval.InteractionRecord per case that reaches a
// terminal state — closing the eval-mirror gap documented in
// bodhyanga/pkg/runtime/doc.go via its option (b): a per-deployment terminal-hop
// subscriber, owned here by the pipeline Runner. Records are keyed on the
// opaque case token; no PHI (research.md R7).
package evalhook

import (
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/google/uuid"
)

// CaseOutcome is the terminal-state summary the Runner assembles.
type CaseOutcome struct {
	CaseToken     string
	Approved      bool // physician approved (or no review gate configured)
	Rejected      bool
	ReviewerID    string
	Diagnosis     string
	Confidence    float64
	Rounds        int
	Forced        bool
	CostUSD       float64 // total virtual test cost for the case (specs/006)
	Diagnostician string  // rule | llm-anthropic | llm-ollama | fake-llm
	StartedAt     time.Time
	EndedAt       time.Time
}

// Record maps the outcome onto bodhyanga's InteractionRecord and saves it.
// Success = approved AND not forced (a forced progression reaching approval
// is still a quality miss worth surfacing).
func Record(store eval.Store, o CaseOutcome) (eval.InteractionRecord, error) {
	forced := 0.0
	if o.Forced {
		forced = 1
	}
	decision := "none"
	switch {
	case o.Rejected:
		decision = "reject"
	case o.Approved && o.ReviewerID != "":
		decision = "approve"
	}
	rec := eval.InteractionRecord{
		ID:       uuid.NewString(),
		Scenario: "mara-slice/" + o.CaseToken,
		Success:  o.Approved && !o.Forced,
		Metrics: map[string]float64{
			"confidence": o.Confidence,
			"rounds":     float64(o.Rounds),
			"forced":     forced,
			"cost_usd":   o.CostUSD,
		},
		Metadata: map[string]any{
			"review_decision": decision,
			"reviewer_id":     o.ReviewerID,
			"diagnostician":   o.Diagnostician,
			"diagnosis":       o.Diagnosis,
		},
		StartedAt: o.StartedAt,
		EndedAt:   o.EndedAt,
	}
	return rec, store.Save(rec)
}
