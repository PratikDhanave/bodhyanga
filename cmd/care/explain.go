// Right-to-explanation handler for Bodh.
//
// GET /explain/{case_id} returns a structured, PHI-free explanation of every
// agent invocation, every governance decision, every human review, and the
// final metrics for a given case. It serves two regulatory regimes at once:
//
//   - GDPR Article 22(3) — when a decision is based on automated processing,
//     the data subject has the right to obtain "human intervention on the
//     part of the controller, to express his or her point of view and to
//     contest the decision." A meaningful explanation is the substrate for
//     contesting the decision; this handler is that substrate.
//
//   - GDPR Article 15 — right of access including "the existence of automated
//     decision-making, including profiling… and, at least in those cases,
//     meaningful information about the logic involved, as well as the
//     significance and the envisaged consequences of such processing for the
//     data subject."
//
// The response is built entirely from the audit log + eval store. The audit
// event schema is deliberately PHI-free ("enough to reconstruct, never enough
// to leak"); this handler inherits that guarantee. The diagnosis label and
// confidence come from the eval store, which records the final case metrics
// but not the underlying narrative — that lives in the persistence layer
// behind separate access controls.
//
// Multi-tenant scoping: the binary's BODH_TENANT_ID gate already scoped the
// audit log it reads from. A future per-request tenant routing change would
// add ?tenant_id= here.

package main

import (
	"net/http"
	"sort"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/hitl"
)

// Explanation is the wire shape returned by GET /explain/{case_id}.
//
// Field selection mirrors the audit event's "enough to reconstruct, never
// enough to leak" rule. Notably absent:
//
//   - msg.Content (chief complaint, HPI, narrative — PHI)
//   - test result strings (lab values, imaging findings — PHI)
//   - patient name / DOB / MRN (PHI)
//
// The final_diagnosis field carries the diagnosis label only when the case
// completed; it is included because that label is the "automated decision"
// the data subject is exercising rights against, and an explanation that
// hides what was decided is not an explanation.
type Explanation struct {
	CaseID      string    `json:"case_id"`
	TenantID    string    `json:"tenant_id,omitempty"`
	GeneratedAt time.Time `json:"generated_at"`

	// Status of the case as derived from the audit + eval records.
	//   pending   — no terminal eval record found, no failure events
	//   completed — eval record present
	//   failed    — agent_error event present without a subsequent completion
	//   unknown   — no events found for this case (404-ish, but with a body)
	Status string `json:"status"`

	// Final decision summary (populated only when status == completed).
	FinalDiagnosis string  `json:"final_diagnosis,omitempty"`
	Accuracy       float64 `json:"accuracy_vs_gold,omitempty"`
	TotalCostUSD   float64 `json:"total_cost_usd,omitempty"`
	UnderBudget    *bool   `json:"under_budget,omitempty"`

	// Reasoning chain — one entry per agent invocation.
	AgentHops []AgentHop `json:"agent_hops"`

	// Human review decisions on this case.
	HumanReviews []HumanReviewSummary `json:"human_reviews"`

	// Policy decisions on this case (deny + allow on clinical types).
	PolicyDecisions []PolicyDecisionSummary `json:"policy_decisions"`

	// Total event count by audit kind — quick triage signal.
	EventCounts map[string]int `json:"event_counts"`

	// Caveats — what this explanation deliberately does NOT contain.
	Caveats []string `json:"caveats"`
}

// AgentHop summarises one agent invocation from the audit log.
type AgentHop struct {
	At        time.Time `json:"at"`
	AgentID   string    `json:"agent_id"`
	MessageID string    `json:"message_id"`
	Type      string    `json:"type"`
	From      string    `json:"from,omitempty"`
	To        string    `json:"to,omitempty"`
	HadError  bool      `json:"had_error,omitempty"`
	ErrorText string    `json:"error_text,omitempty"`
}

// HumanReviewSummary summarises one review decision. The reviewer's rationale
// (`Reason`) is preserved because it is the reviewer's clinical reasoning,
// not patient PHI. The HITL UI guidance trains reviewers to keep rationales
// PHI-free; PHI redaction at the logger seam is the defence-in-depth.
type HumanReviewSummary struct {
	At         time.Time `json:"at,omitempty"`
	Kind       string    `json:"kind"`
	Status     string    `json:"status"`
	Decision   string    `json:"decision,omitempty"`
	ReviewerID string    `json:"reviewer_id,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Priority   string    `json:"priority,omitempty"`
}

// PolicyDecisionSummary summarises one governance decision on this case.
type PolicyDecisionSummary struct {
	At          time.Time `json:"at"`
	Decision    string    `json:"decision"`
	Reason      string    `json:"reason,omitempty"`
	MessageType string    `json:"message_type,omitempty"`
}

// explainHandler returns a GET /explain/{case_id} HTTP handler.
//
// The handler is intentionally read-only against an audit.Recorder and
// eval.Store. It does not consult the patient registry, the persistence
// layer's PHI columns, or the HITL queue's payload bodies. Everything in
// the response derives from PHI-free sources.
//
// The hitl.Queue lookup is optional; when it's nil the handler degrades to
// the audit-only view (review events still appear via KindReviewDecision).
func explainHandler(tenantID string, rec audit.Recorder, store eval.Store, queue *hitl.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caseID := r.PathValue("case_id")
		if caseID == "" {
			http.Error(w, "case_id path parameter required", http.StatusBadRequest)
			return
		}

		exp := Explanation{
			CaseID:      caseID,
			TenantID:    tenantID,
			GeneratedAt: time.Now().UTC(),
			Status:      "unknown",
			AgentHops:   []AgentHop{},
			HumanReviews:    []HumanReviewSummary{},
			PolicyDecisions: []PolicyDecisionSummary{},
			EventCounts:     map[string]int{},
			Caveats: []string{
				"This explanation is derived from the audit log + eval store. " +
					"It does NOT include patient narrative, test result values, " +
					"medication names/doses, or any other PHI; those live behind " +
					"separate access controls.",
				"Snapshot generated at the time of this request; subsequent events " +
					"are not reflected.",
				"Bodh is a research and engineering reference. Not a medical " +
					"device, not approved for clinical use.",
			},
		}

		// Pass 1: audit events for this case.
		events := rec.List()
		hadError := false
		for _, e := range events {
			if e.CaseID != caseID {
				continue
			}
			exp.EventCounts[string(e.Kind)]++

			switch e.Kind {
			case audit.KindMessage:
				exp.AgentHops = append(exp.AgentHops, AgentHop{
					At:        e.At,
					AgentID:   e.AgentID,
					MessageID: e.MessageID,
					Type:      e.Type,
					From:      e.From,
					To:        e.To,
				})
			case audit.KindAgentError:
				hadError = true
				exp.AgentHops = append(exp.AgentHops, AgentHop{
					At:        e.At,
					AgentID:   e.AgentID,
					MessageID: e.MessageID,
					Type:      e.Type,
					HadError:  true,
					ErrorText: e.ErrorText,
				})
			case audit.KindPolicyDecision:
				exp.PolicyDecisions = append(exp.PolicyDecisions, PolicyDecisionSummary{
					At:          e.At,
					Decision:    e.Decision,
					Reason:      e.Reason,
					MessageType: e.Type,
				})
			case audit.KindReviewDecision:
				exp.HumanReviews = append(exp.HumanReviews, HumanReviewSummary{
					At:         e.At,
					Kind:       e.Type,
					Status:     "decided",
					Decision:   e.Decision,
					ReviewerID: e.ReviewerID,
					Reason:     e.Reason,
				})
			}
		}

		// Pass 2: eval record (final metrics) for this case.
		// InteractionRecord.ID is the case_id.
		if store != nil {
			for _, rec := range store.List() {
				if rec.ID != caseID {
					continue
				}
				exp.Status = "completed"
				if rec.Metrics != nil {
					exp.Accuracy = rec.Metrics["accuracy"]
					exp.TotalCostUSD = rec.Metrics["cost_usd"]
				}
				if rec.Metadata != nil {
					if v, ok := rec.Metadata["under_budget"].(bool); ok {
						exp.UnderBudget = &v
					}
					// final_diagnosis is intentionally not in the InteractionRecord
					// (PHI-adjacent in the demo's gold_diagnosis); leave it empty
					// here and let operators wire a separate signal if their
					// deployment policy permits it.
					_ = rec.Metadata
				}
				break
			}
		}

		// Pass 3: optional in-process HITL queue lookup for pending reviews
		// that haven't yet produced KindReviewDecision events (i.e. still
		// awaiting clinician disposition).
		if queue != nil {
			for _, rr := range queue.List() {
				if rr.CaseID != caseID {
					continue
				}
				summary := HumanReviewSummary{
					Kind:     rr.Kind,
					Status:   string(rr.Status),
					Priority: rr.Priority,
				}
				if !rr.DecidedAt.IsZero() {
					summary.At = rr.DecidedAt
					summary.Decision = rr.Reason // queue's Reason is the rationale
					summary.ReviewerID = rr.ReviewerID
				} else {
					summary.At = rr.SubmittedAt
				}
				// Avoid double-counting reviews that also appear in the audit
				// log as KindReviewDecision events.
				if !haveReviewAlready(exp.HumanReviews, rr.ID, summary.At) {
					exp.HumanReviews = append(exp.HumanReviews, summary)
				}
			}
		}

		// Status resolution when no eval record was found.
		if exp.Status == "unknown" {
			switch {
			case hadError:
				exp.Status = "failed"
			case len(exp.AgentHops) > 0:
				exp.Status = "pending"
			}
		}

		// Sort the chronological collections so the response is reproducible.
		sort.Slice(exp.AgentHops, func(i, j int) bool {
			return exp.AgentHops[i].At.Before(exp.AgentHops[j].At)
		})
		sort.Slice(exp.HumanReviews, func(i, j int) bool {
			return exp.HumanReviews[i].At.Before(exp.HumanReviews[j].At)
		})
		sort.Slice(exp.PolicyDecisions, func(i, j int) bool {
			return exp.PolicyDecisions[i].At.Before(exp.PolicyDecisions[j].At)
		})

		writeJSON(w, exp)
	}
}

// haveReviewAlready dedupes when a queue entry is also recorded as a
// KindReviewDecision audit event.
func haveReviewAlready(reviews []HumanReviewSummary, _ string, at time.Time) bool {
	for _, r := range reviews {
		if r.At.Equal(at) {
			return true
		}
	}
	return false
}
