package agents

// File human_review.go: the human_review agent — the Bodh platform's
// HITL gate. This file is the architectural expression of the 21 USC
// §360j(o)(1) Clinical Decision Support carve-out **criterion 4**:
//
//	"enable [the clinician] to independently review the basis for the
//	 recommendations."
//
// See bodh-linkedin/02-cures-act-as-code.md for the regulatory
// background and docs/architecture.md § Human-in-the-loop pattern for the
// full envelope spec.
//
// The contract this file enforces:
//
//   1. An upstream agent that wants to gate a clinical action does NOT
//      send the payload to its intended next agent. It wraps the payload
//      and addresses the message to "human_review" with type
//      "review_request" plus six envelope keys in Metadata (hitl_kind,
//      hitl_next_to, hitl_next_type, hitl_priority, hitl_subject,
//      hitl_sla_minutes).
//
//   2. HandleMessage parks the request in the hitl.Queue and emits NO
//      follow-up messages. The system halts until a reviewer decides.
//      There is no path that bypasses the gate — that's what
//      "consequential" review means under criterion 4.
//
//   3. A reviewer calls ApplyDecision (directly, or via the HTTP
//      handler at POST /reviews/{id}/decide). On approve, the original
//      payload is republished to hitl_next_to with type hitl_next_type.
//      **On reject, no message is returned and no clinical action
//      flows.** That's the "lossless on reject" guarantee.
//
// GDPR Article 22 ("not subject to a decision based solely on automated
// processing") is satisfied by the same architecture. One gate, two
// regulatory regimes covered. See bodh-linkedin/02-cures-act-as-code.md.

import (
	"context"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// HumanReviewAgent is the platform's human-in-the-loop (HITL) gate.
// Clinical agents that need oversight emit a message of type
// "review_request" with HITL envelope fields in Metadata; this agent
// records the request in the queue and emits no follow-ups. When a
// reviewer decides via ApplyDecision, the original payload is resumed
// onto the bus toward its intended recipient.
//
// Envelope metadata keys set by the upstream agent:
//
//	hitl_kind         string  (required) e.g. "diagnosis_review", "care_plan_review"
//	hitl_next_to      string  (required) agent id to forward to on approval
//	hitl_next_type    string  (required) message type to use on resume
//	hitl_priority     string  (optional) low|normal|high|urgent (default normal)
//	hitl_subject      string  (optional) short label shown in the queue
//	hitl_sla_minutes  float64 (optional) deadline in minutes; expired = never resume
//
// The seven agents that gate today (PR #6+):
//   - reasoning_verifier  → diagnosis_review
//   - cdm_planner         → care_plan_review
//   - tcm_coordinator     → tcm_welcome_review
//   - panel_manager       → outreach_review (per-gap)
//   - refill_manager      → refill_review
//   - prior_auth          → prior_auth_review
//   - readmission_tracker → discharge_review (priority scales with risk band)
//
// See bodh-linkedin/02-cures-act-as-code.md § Seven gated agents and
// docs/agents.md § HITL.
type HumanReviewAgent struct {
	// Queue is the review store this agent parks requests in and applies
	// decisions to. It is the hitl.ReviewStore interface, so cmd/care can
	// inject the in-memory *hitl.Queue while a decoupled deployment injects
	// a Postgres-backed store — the same store the backend HTTP handlers
	// read for List / Pending visibility.
	Queue hitl.ReviewStore
}

// ID matches what upstream agents address their review_request messages
// to. Hardcoded in every gating agent (e.g. reasoning_verifier.go,
// cdm_planner.go) so changing this string is a breaking change across
// the whole HITL surface.
func (a *HumanReviewAgent) ID() string   { return "human_review" }
func (a *HumanReviewAgent) Name() string { return "Human-in-the-Loop Reviewer" }
func (a *HumanReviewAgent) Capabilities() []string {
	return []string{"human_review", "clinical_oversight"}
}

// envelopeKeys are stripped from Metadata before the resumed message is
// republished, so downstream agents never see HITL bookkeeping. After
// ApplyDecision approves, the next agent receives the original payload
// with the SAME metadata the upstream agent constructed (case_id,
// tenant_id, trace_id, patient_id) but WITHOUT the hitl_* envelope —
// from the downstream agent's perspective the message looks just like
// any other agent-to-agent hop. That preserves the design rule that
// agents don't know whether they're behind a gate.
var envelopeKeys = []string{
	"hitl_kind", "hitl_next_to", "hitl_next_type",
	"hitl_priority", "hitl_subject", "hitl_sla_minutes",
}

// HandleMessage parks the review_request in the queue and returns
// nothing — the system halts here. The reviewer's decision (via
// ApplyDecision) is what produces the resume messages.
//
// Returning ([]agent.Message{}, nil) — i.e. zero outputs — is what
// implements the gate. The orchestrator publishes whatever this returns;
// returning nothing means nothing flows downstream. The case is frozen
// until a human acts.
//
// Validation: kind / next_to / next_type are all required. If any are
// missing the envelope is malformed and we drop the message with a log
// line (still no audit gap because the orchestrator's KindMessage
// already recorded the inbound). We do NOT return an error — that would
// trigger a KindAgentError which is misleading: the agent isn't broken,
// the caller sent a bad envelope.
func (a *HumanReviewAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	// Pull the three required envelope keys. metadataString returns ""
	// for missing or wrong-type values, so a single empty-string check
	// covers both.
	kind := metadataString(msg.Metadata, "hitl_kind")
	nextTo := metadataString(msg.Metadata, "hitl_next_to")
	nextType := metadataString(msg.Metadata, "hitl_next_type")
	if kind == "" || nextTo == "" || nextType == "" {
		// Malformed envelope: log and drop. The reviewer can't act
		// on a request that doesn't know where it's going. This
		// path is rare in practice (every gating agent uses the
		// reviewEnvelope helper below which always sets all three)
		// but defended for the case where a buggy new agent forgets
		// one of the keys.
		env.Logf("[human_review] envelope missing kind/next_to/next_type; dropping %s", msg.ID)
		return nil, nil
	}
	// Optional envelope keys with defaults.
	subject := metadataString(msg.Metadata, "hitl_subject")
	if subject == "" {
		// Default subject = kind so the reviewer's UI always has
		// some label even if the upstream agent forgot to set one.
		subject = kind
	}
	priority := metadataString(msg.Metadata, "hitl_priority")
	if priority == "" {
		// "normal" is the safe default — neither paged out loud
		// nor buried at the bottom of the queue.
		priority = "normal"
	}
	// SLA: hitl_sla_minutes is a relative deadline in minutes from
	// now. We store the absolute time so the Queue.Expire scan can
	// compare against a stable target across timezone changes /
	// clock skew. Zero means "no SLA — never expire". The Queue's
	// Expire mechanism transitions pending requests past their SLA
	// to status=expired, and expired requests NEVER resume (lossless
	// on expiry, just like reject). See pkg/hitl/queue.go Expire.
	var sla time.Time
	if mins := metadataFloat(msg.Metadata, "hitl_sla_minutes", 0); mins > 0 {
		sla = time.Now().Add(time.Duration(mins) * time.Minute)
	}
	// Strip envelope keys from the metadata that will travel with
	// the resumed message. Downstream agents must not see HITL
	// bookkeeping in their inbound — see envelopeKeys comment.
	forwardMeta := copyMeta(msg.Metadata)
	for _, k := range envelopeKeys {
		delete(forwardMeta, k)
	}
	// Park the request in the queue. From here the system halts
	// until a reviewer calls ApplyDecision (or HTTP POST
	// /reviews/{id}/decide which calls ApplyDecision internally).
	// SubmittedBy is the upstream agent's ID — auditable provenance
	// for who asked for review of what.
	req := a.Queue.Submit(medical.ReviewRequest{
		Kind:      kind,
		PatientID: metadataString(msg.Metadata, "patient_id"),
		CaseID:    metadataString(msg.Metadata, "case_id"),
		Priority:  priority,
		Subject:   subject,
		// Detail and Payload both carry the original Content. Detail
		// is what the reviewer's UI shows (may be reformatted by the
		// HTTP layer); Payload is the byte-for-byte payload that
		// gets republished on approve.
		Detail:      msg.Content,
		NextTo:      nextTo,
		NextType:    nextType,
		Payload:     msg.Content,
		Meta:        forwardMeta,
		SLAAt:       sla,
		SubmittedBy: msg.From,
	})
	env.Logf("[human_review] pending id=%s kind=%s patient=%s priority=%s next=%s/%s",
		req.ID, kind, req.PatientID, priority, nextTo, nextType)
	// Return no messages. The case is now in human hands. Returning
	// nil here is the load-bearing part of the criterion 4 contract:
	// no downstream action flows until a reviewer decides.
	return nil, nil
}

// ApplyDecision records a reviewer's decision and returns the resume
// messages the caller should publish on the bus. On approve /
// approve_with_modifications, the original payload (optionally replaced
// by `modification`) is sent to NextTo with NextType. On reject, no
// message is returned. ok=false if the id is unknown, already decided,
// or the decision string is invalid.
//
// **Lossless on reject — the load-bearing property.** When the reviewer
// rejects, this function returns (request, nil, true). The caller (HTTP
// handler in cmd/care, or cmd/caredemo's auto-approver) sees the empty
// message slice and publishes nothing. The original payload was NEVER
// emitted to its intended recipient — no SMS sent, no PA submitted, no
// TCM cascade started. The audit log records a KindReviewDecision with
// decision=reject and the reviewer's Reason, and that's the only
// artefact that survives. See bodh-linkedin/02-cures-act-as-code.md
// § "Lossless on reject" is the key property.
//
// SLA expiry has the same shape: an expired request can never be
// approved, so it can never resume. The audit log captures the expiry,
// the queue retains the entry with status=expired for reviewer
// visibility (so it doesn't silently vanish), but no clinical action
// flows. Same property as reject.
//
// approve_with_modifications: when the reviewer wants to ship a tweaked
// version of the payload (e.g. care-plan spirometry cadence changed
// from weekly to monthly), they pass the new payload as `modification`.
// We republish THAT instead of the original — the reviewer's edit is
// authoritative. This is also explicitly recorded in the audit event so
// future reviewers can see what was changed and why.
func (a *HumanReviewAgent) ApplyDecision(id, reviewerID, decision, reason, modification string) (medical.ReviewRequest, []agent.Message, bool) {
	// Delegate to the Queue to validate the decision string ("approve"
	// / "approve_with_modifications" / "reject") and to flip the
	// status atomically under the queue's lock. If the id is unknown
	// or already decided, ok=false and we return early without
	// emitting anything.
	r, ok := a.Queue.Decide(id, reviewerID, decision, reason, modification)
	if !ok {
		return r, nil, false
	}
	// Build the resume message from the decided review. The construction
	// half now lives in pkg/hitl.BuildResumeMessage — the same agent-free
	// implementation the decoupled backend (cmd/api) uses — so the monolith
	// and the backend cannot drift in how an approved review resumes the
	// pipeline. resume=false on a reject (or any non-approving terminal
	// status); ok stays true because the decision itself was applied
	// successfully ("ok" measures the apply, not the approval).
	msg, resume := hitl.BuildResumeMessage(r, reviewerID, modification)
	if !resume {
		return r, nil, true
	}
	return r, []agent.Message{msg}, true
}

// reviewOptions configures the HITL gate wrapped around a downstream
// message. Used by reviewEnvelope below, which every gating agent (the
// seven listed in the HumanReviewAgent doc comment) calls when HITL is
// true to convert a normal outbound message into a review_request
// addressed to "human_review".
type reviewOptions struct {
	// Kind = the hitl_kind envelope key. Drives the reviewer's UI
	// filter ("show me only diagnosis_review items") and per-kind
	// reject-rate metrics.
	Kind string
	// NextTo / NextType = where + how the resumed message is sent on
	// approve. Together they fully describe the action the reviewer
	// is gating.
	NextTo   string
	NextType string
	// Priority — tier the queue. high / urgent should be paged out
	// loud; low / normal wait in the queue.
	Priority string
	// Subject — short human label for the queue UI.
	Subject string
	// SLAMinutes — wall-clock deadline. Past this, the request
	// transitions to status=expired and can never resume. Used for
	// time-critical flows (sepsis alerts, readmission discharge
	// handoffs) where a stale approval is worse than no approval.
	SLAMinutes float64
}

// reviewEnvelope adds HITL envelope keys to a copy of meta, so the
// upstream agent can address the message to "human_review" with type
// "review_request" while preserving the original target in Metadata.
//
// Usage pattern from any gating agent:
//
//	if a.HITL {
//	    meta := reviewEnvelope(msg.Metadata, reviewOptions{
//	        Kind:     "care_plan_review",
//	        NextTo:   "engagement",
//	        NextType: "care_plan_ready",
//	        Priority: "normal",
//	        Subject:  fmt.Sprintf("care plan for %s", patientID),
//	    })
//	    return []agent.Message{
//	        agent.NewMessage(a.ID(), "human_review", agent.RoleAgent,
//	            "review_request", carePlanJSON, meta),
//	    }, nil
//	}
//
// The shallow-copy is required so the upstream agent's own metadata is
// not mutated — see copyMeta rationale in agents/supervisor.go.
func reviewEnvelope(meta map[string]any, opts reviewOptions) map[string]any {
	m := copyMeta(meta)
	m["hitl_kind"] = opts.Kind
	m["hitl_next_to"] = opts.NextTo
	m["hitl_next_type"] = opts.NextType
	// Optional fields: only set when present so absence vs explicit
	// default is meaningful at the HandleMessage default-resolution
	// layer.
	if opts.Priority != "" {
		m["hitl_priority"] = opts.Priority
	}
	if opts.Subject != "" {
		m["hitl_subject"] = opts.Subject
	}
	if opts.SLAMinutes > 0 {
		m["hitl_sla_minutes"] = opts.SLAMinutes
	}
	return m
}
