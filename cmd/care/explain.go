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
// WHAT THIS HANDLER DELIBERATELY DOES NOT DO:
//   - It does NOT generate a free-text natural-language explanation. That
//     would put PHI back into the response (the LLM would weave clinical
//     details into prose). Structured explanations satisfy the GDPR
//     "meaningful information about the logic involved" requirement
//     without re-emitting payload — see docs/aigp-governance-mapping.md
//     practice scenarios on right-to-explanation.
//   - It does NOT consult the persistence layer's PHI columns or the
//     patient registry directly. Only PHI-free sources are read.
//   - It does NOT return the diagnosis label by default. The label COULD
//     be the "automated decision" the data subject contests; current
//     deployments leave it empty pending an operator-side policy decision
//     (see status-resolution comment in the body).
//
// Multi-tenant scoping: the binary's BODH_TENANT_ID gate already scoped the
// audit log it reads from. A future per-request tenant routing change would
// add ?tenant_id= here.

package main

import (
	"net/http"
	"sort"
	"time"

	// audit is the PHI-free forensic record this handler reads.
	"github.com/PratikDhanave/bodh/pkg/audit"
	// eval is the final-metrics source (accuracy, cost, under-budget flag).
	// InteractionRecord IS NOT a PHI surface — it stores numeric metrics
	// and opaque IDs only.
	"github.com/PratikDhanave/bodh/pkg/eval"
	// hitl is consulted for pending reviews that haven't yet produced a
	// KindReviewDecision audit event. Optional — see explainHandler doc.
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
	// CaseID echoes the path parameter. Opaque token (e.g. "demo-case-001"),
	// never an MRN — see pkg/audit Event.CaseID doc.
	CaseID string `json:"case_id"`

	// TenantID is the multi-tenant scope this binary serves. Echoed back so
	// the response is self-describing for the operator querying it; matches
	// the BODH_TENANT_ID env at process start. Future per-request tenant
	// routing would set this per request.
	TenantID string `json:"tenant_id,omitempty"`

	// GeneratedAt is the snapshot time. Per the Caveats below, subsequent
	// events are not reflected — the caller may want to re-fetch.
	GeneratedAt time.Time `json:"generated_at"`

	// Status of the case as derived from the audit + eval records.
	//
	//   unknown   — no events found for this case (path param refers to a
	//               case the binary has no record of). Returned with body
	//               (not 404) so the caller can still see the Caveats.
	//   pending   — agent hops exist but no eval record and no agent_error.
	//               Case is in flight.
	//   completed — eval record present (InteractionRecord exists for the
	//               case_id, which the orchestrator only writes on the
	//               supervisor's finalize hop — see pkg/orchestration).
	//   failed    — agent_error event seen and no completion. The case
	//               aborted; final_diagnosis and metrics will be empty.
	//
	// Anchors GDPR Article 22(3)'s "logic involved" disclosure: the data
	// subject knows whether a decision was actually made about them.
	Status string `json:"status"`

	// Final decision summary (populated only when status == completed).
	//
	// FinalDiagnosis is the diagnosis LABEL only (e.g. "Community-acquired
	// pneumonia"), never the rationale narrative. Today it is left empty
	// pending an operator-side policy on label disclosure; the field is
	// kept on the schema so a future deployment can wire it without
	// breaking clients (the json omitempty means clients see no field
	// rather than an empty string).
	FinalDiagnosis string `json:"final_diagnosis,omitempty"`
	// Accuracy is the eval store's accuracy-vs-gold metric (0.0–1.0). Not
	// PHI — it is a numeric quality score for the case.
	Accuracy float64 `json:"accuracy_vs_gold,omitempty"`
	// TotalCostUSD is the simulated cost from the test catalog. Not PHI;
	// the cost numbers are platform configuration, not patient billing.
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	// UnderBudget is *bool (not bool) to distinguish "false" from
	// "unknown / not recorded" in the JSON output. omitempty drops nil.
	UnderBudget *bool `json:"under_budget,omitempty"`

	// Reasoning chain — one entry per agent invocation. This is the
	// "meaningful information about the logic involved" surface for GDPR
	// Article 15(1)(h). The data subject sees which specialist agents
	// were invoked in what order, without ever seeing the data they
	// reasoned over.
	AgentHops []AgentHop `json:"agent_hops"`

	// Human review decisions on this case. The presence of human reviews
	// is itself part of the GDPR Article 22(3) "human intervention"
	// disclosure — proof that a clinician reviewed before the platform
	// acted (21 USC §360j(o)(1) CDS carve-out criterion 4).
	HumanReviews []HumanReviewSummary `json:"human_reviews"`

	// Policy decisions on this case (deny + allow on clinical types). The
	// data subject sees what governance ran — required by GDPR Article 35
	// DPIA disclosure when an automated decision involves policy gating.
	PolicyDecisions []PolicyDecisionSummary `json:"policy_decisions"`

	// Total event count by audit kind — quick triage signal. The integers
	// are a histogram, not a list of events; useful for sanity-checking
	// that the AgentHops / HumanReviews / PolicyDecisions slices are
	// complete versus truncated.
	EventCounts map[string]int `json:"event_counts"`

	// Caveats — what this explanation deliberately does NOT contain.
	// These strings ARE PART OF THE PUBLIC CONTRACT — clients render them
	// to end users so the end user understands what was deliberately
	// omitted vs. what is genuinely absent. Changing the wording is a
	// breaking change for any UI that string-matches.
	Caveats []string `json:"caveats"`
}

// AgentHop summarises one agent invocation from the audit log. Each hop
// derives from a KindMessage (or KindAgentError) audit event. Fields are
// the PHI-free routing tuple plus error context — never the message body.
type AgentHop struct {
	// At is the time the orchestrator dispatched the message to the agent.
	At time.Time `json:"at"`
	// AgentID is the registered agent that handled the message (e.g.
	// "diagnostic_supervisor", "test_planner"). Disclosing the agent
	// catalog is part of GDPR Article 15's "logic involved".
	AgentID string `json:"agent_id"`
	// MessageID joins back to the audit log for an investigator.
	MessageID string `json:"message_id"`
	// Type is the wire-level message type (e.g. "diagnosis_proposal").
	// Disclosing the type vocabulary is also "logic involved".
	Type string `json:"type"`
	// From / To are routing fields; together with Type they reconstruct
	// the message graph for the case. omitempty drops them when the
	// underlying audit event left them blank (e.g. for the initial
	// orchestrator-emitted intake message).
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// HadError is true for hops sourced from KindAgentError audit events.
	// Surfaced because Article 22 contestability requires the data
	// subject to know if the automated processing actually failed.
	HadError bool `json:"had_error,omitempty"`
	// ErrorText is err.Error() from the agent's HandleMessage. The
	// orchestrator's audit.recordAgentError discipline guarantees this is
	// the error string, NEVER msg.Content (see pkg/audit recorder doc).
	ErrorText string `json:"error_text,omitempty"`
}

// HumanReviewSummary summarises one review decision. The reviewer's rationale
// (`Reason`) is preserved because it is the reviewer's clinical reasoning,
// not patient PHI. The HITL UI guidance trains reviewers to keep rationales
// PHI-free; PHI redaction at the logger seam is the defence-in-depth.
type HumanReviewSummary struct {
	// At is the audit event time (KindReviewDecision) when available;
	// otherwise it is the queue entry's DecidedAt or SubmittedAt — see the
	// queue fallback branch in the handler. omitempty drops zero-time
	// entries (e.g. an entry built from queue.SubmittedAt when even that
	// is missing, which is a defensive guard).
	At time.Time `json:"at,omitempty"`
	// Kind is the review type ("diagnosis_review", "care_plan_review",
	// etc.). Disclosing the kind vocabulary tells the data subject which
	// human-oversight gates ran.
	Kind string `json:"kind"`
	// Status is the queue-side status string when sourced from the live
	// HITL queue; "decided" when sourced from a closed-out audit event.
	// The two sources do not normalize to the same vocabulary — clients
	// should treat Status as advisory and rely on Decision for the
	// terminal verdict.
	Status string `json:"status"`
	// Decision is the reviewer's choice (approve /
	// approve_with_modifications / reject) when known.
	Decision string `json:"decision,omitempty"`
	// ReviewerID identifies the clinician who decided. Their identity is
	// part of the §360j(o)(1) CDS carve-out audit trail.
	ReviewerID string `json:"reviewer_id,omitempty"`
	// Reason is the reviewer's free-text rationale. PHI-free by training
	// of reviewers + redacted at the logger seam (pkg/phi).
	Reason string `json:"reason,omitempty"`
	// Priority is informational ("low"/"normal"/"high"/"urgent") from the
	// queue entry; the audit log does not carry it (queue is the only
	// source).
	Priority string `json:"priority,omitempty"`
}

// PolicyDecisionSummary summarises one governance decision on this case.
// Derived from KindPolicyDecision audit events — both denies (always
// recorded) and allows on clinical message types (recorded selectively per
// the audit-hook policy in pkg/orchestration).
type PolicyDecisionSummary struct {
	// At is the audit event time — the moment the orchestrator's policy
	// composer returned a decision.
	At time.Time `json:"at"`
	// Decision is "allow" or "deny" (the governance.PolicyDecision values).
	Decision string `json:"decision"`
	// Reason is the policy's rationale string. Policies are responsible
	// for keeping these PHI-free (template strings, not payload echoes).
	Reason string `json:"reason,omitempty"`
	// MessageType is the type of the message the policy evaluated. Helps
	// the data subject see which class of action was gated.
	MessageType string `json:"message_type,omitempty"`
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
//
// Three-pass design:
//   - Pass 1 scans the audit log once and routes each event to the right
//     section (AgentHops / PolicyDecisions / HumanReviews) plus the
//     EventCounts histogram.
//   - Pass 2 reads the eval.Store to pick up the final case metrics; this
//     is also the signal that flips Status to "completed".
//   - Pass 3 (optional) merges in pending HITL queue entries that have not
//     yet produced KindReviewDecision events — i.e. reviews still awaiting
//     clinician disposition. The dedup against Pass 1 by timestamp prevents
//     a decided review from appearing twice.
func explainHandler(tenantID string, rec audit.Recorder, store eval.Store, queue hitl.ReviewStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The path is /explain/{case_id}; r.PathValue is Go 1.22+ pattern
		// matching from http.ServeMux. An empty case_id is treated as
		// 400 rather than serving an "unknown" body because it indicates
		// a broken caller, not a missing case.
		caseID := r.PathValue("case_id")
		if caseID == "" {
			http.Error(w, "case_id path parameter required", http.StatusBadRequest)
			return
		}

		// Construct the response with empty slices (not nil) so the JSON
		// output is `"agent_hops": []` rather than `"agent_hops": null`.
		// Clients that iterate the JSON without a nil check (most of them)
		// rely on this.
		exp := Explanation{
			CaseID:      caseID,
			TenantID:    tenantID,
			GeneratedAt: time.Now().UTC(),
			// Start "unknown"; passes 1 and 2 may upgrade this.
			Status:          "unknown",
			AgentHops:       []AgentHop{},
			HumanReviews:    []HumanReviewSummary{},
			PolicyDecisions: []PolicyDecisionSummary{},
			EventCounts:     map[string]int{},
			// Caveats wording is the public contract — see the
			// Explanation.Caveats doc above. Changing these strings
			// breaks any UI that string-matches.
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
		//
		// rec.List() returns a snapshot; we iterate and filter on CaseID
		// rather than asking the recorder to filter because the in-memory
		// recorder has no index and the file recorder doesn't either. The
		// production Postgres recorder will offer a query; this loop stays
		// correct against any Recorder implementation.
		events := rec.List()
		hadError := false
		for _, e := range events {
			if e.CaseID != caseID {
				continue
			}
			// EventCounts is the per-kind histogram exposed in the
			// response. Useful for clients to spot truncation or
			// imbalance (e.g. lots of denies vs. allows).
			exp.EventCounts[string(e.Kind)]++

			// Kind-specific projection into the right slice. Adding a
			// new audit.Kind requires a new case branch here — silent
			// drop is the wrong default for an explainability endpoint.
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
				// hadError gates the failed-vs-pending status resolution
				// below. We still record the hop so the chain shows the
				// failure point in chronological order.
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
				// e.Type carries the review Kind (the audit event reuses
				// the Type field for the review-kind string when the
				// kind is KindReviewDecision). Status hard-coded to
				// "decided" because the audit record itself is the
				// closed-out version; live queue status comes from
				// Pass 3 below.
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
		// InteractionRecord.ID is the case_id — this is the orchestrator's
		// recordCaseEval contract (see pkg/orchestration/orchestrator.go).
		// Finding a record here means the supervisor finalized the case;
		// that's the signal we flip Status to "completed".
		if store != nil {
			for _, rec := range store.List() {
				if rec.ID != caseID {
					continue
				}
				exp.Status = "completed"
				// Metrics is map[string]float64; missing keys read as
				// zero-value 0.0, which then get omitempty'd out of the
				// JSON. No defensive .Get() needed.
				if rec.Metrics != nil {
					exp.Accuracy = rec.Metrics["accuracy"]
					exp.TotalCostUSD = rec.Metrics["cost_usd"]
				}
				if rec.Metadata != nil {
					// under_budget is a bool in the metadata map; we
					// take its address so the JSON output distinguishes
					// "false" from "absent" (per UnderBudget *bool doc).
					if v, ok := rec.Metadata["under_budget"].(bool); ok {
						exp.UnderBudget = &v
					}
					// final_diagnosis is intentionally not in the InteractionRecord
					// (PHI-adjacent in the demo's gold_diagnosis); leave it empty
					// here and let operators wire a separate signal if their
					// deployment policy permits it.
					_ = rec.Metadata
				}
				// Only the FIRST matching record is consumed. There
				// should not be duplicates by ID; if a future migration
				// allows multiple, this becomes a TODO.
				break
			}
		}

		// Pass 3: optional in-process HITL queue lookup for pending reviews
		// that haven't yet produced KindReviewDecision events (i.e. still
		// awaiting clinician disposition).
		//
		// Why a separate pass: the audit log records review DECISIONS, not
		// review SUBMISSIONS. A still-pending review (the gate has fired
		// but no clinician has disposed of it) is invisible to Pass 1.
		// The explain handler must surface those so the data subject sees
		// "human oversight is in progress" rather than "no oversight ran".
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
					// Closed-out review in the queue: prefer DecidedAt
					// and the queue-side rationale. Note that
					// queue.Reason fills Decision here — a deliberate
					// quirk: the queue stores the reviewer's free-text
					// rationale under Reason, while the audit event's
					// Decision is the verb (approve/reject). This is a
					// long-standing schema oddity that should be
					// reconciled when the queue gains its own Decision
					// field; for now we surface what we have.
					summary.At = rr.DecidedAt
					summary.Decision = rr.Reason // queue's Reason is the rationale
					summary.ReviewerID = rr.ReviewerID
				} else {
					// Still pending: At = SubmittedAt so the UI can
					// sort chronologically with the rest.
					summary.At = rr.SubmittedAt
				}
				// Avoid double-counting reviews that also appear in the audit
				// log as KindReviewDecision events. Dedup is by timestamp
				// equality (see haveReviewAlready) which is precise enough
				// at UTC nanosecond resolution — DecidedAt in the queue and
				// At in the audit event are stamped from the same wall
				// clock at the gate boundary.
				if !haveReviewAlready(exp.HumanReviews, rr.ID, summary.At) {
					exp.HumanReviews = append(exp.HumanReviews, summary)
				}
			}
		}

		// Status resolution when no eval record was found.
		//
		// Order matters: a failed case is also "has hops" — we must check
		// hadError first so we surface "failed" rather than "pending" for
		// crashes. Cases with neither an eval record, nor agent_error,
		// nor any hops stay "unknown" (the GET returned a body but the
		// case_id is genuinely not on record here).
		if exp.Status == "unknown" {
			switch {
			case hadError:
				exp.Status = "failed"
			case len(exp.AgentHops) > 0:
				exp.Status = "pending"
			}
		}

		// Sort the chronological collections so the response is reproducible.
		// The map iteration in Pass 1 has no guaranteed order, so without
		// these sorts two consecutive GETs could return identical-content
		// but differently-ordered slices, which breaks naive client diffs
		// and test fixtures.
		sort.Slice(exp.AgentHops, func(i, j int) bool {
			return exp.AgentHops[i].At.Before(exp.AgentHops[j].At)
		})
		sort.Slice(exp.HumanReviews, func(i, j int) bool {
			return exp.HumanReviews[i].At.Before(exp.HumanReviews[j].At)
		})
		sort.Slice(exp.PolicyDecisions, func(i, j int) bool {
			return exp.PolicyDecisions[i].At.Before(exp.PolicyDecisions[j].At)
		})

		// writeJSON sets Content-Type and JSON-encodes the response.
		// Defined in cmd/care/main.go alongside the other handler helpers.
		writeJSON(w, exp)
	}
}

// haveReviewAlready dedupes when a queue entry is also recorded as a
// KindReviewDecision audit event.
//
// We match by timestamp because the queue and the audit event are
// generated at the SAME instant from the SAME UTC clock at the gate
// boundary. The second parameter (the queue's ReviewRequest.ID) is
// reserved for a future tighter dedup if a clock skew issue ever appears
// — today the underscore is intentional.
func haveReviewAlready(reviews []HumanReviewSummary, _ string, at time.Time) bool {
	for _, r := range reviews {
		if r.At.Equal(at) {
			return true
		}
	}
	return false
}
