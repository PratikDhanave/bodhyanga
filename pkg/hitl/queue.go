// Package hitl implements the human-in-the-loop review queue. Clinical
// actions that must be reviewed before the platform acts on them are stored
// here as medical.ReviewRequest; a reviewer (clinician or nurse) decides via
// HTTP or programmatic API, and the gate agent resumes the original action.
//
// REGULATORY ANCHOR: this package is the code-level expression of
// 21 USC §360j(o)(1) — the Clinical Decision Support carve-out from the
// FDA's medical-device definition — and specifically criterion 4: the
// software must "enable the health care professional to independently
// review the basis for the recommendations." Without an enforced review
// gate the platform tips into being a regulated medical device under FDA
// jurisdiction. See bodh-linkedin/02-cures-act-as-code.md for the long
// write-up; docs/architecture.md § Human-in-the-loop pattern for the
// in-tree summary including the envelope-key catalog and the gated-agent
// inventory.
//
// LOSSLESS-ON-REJECT contract: on reject, the original payload is recorded
// as a KindReviewDecision audit event and dropped — never republished onto
// the bus. This is the "no half-completed clinical action can leak past
// the gate" guarantee that makes the gate meaningful; without it a
// rejected diagnosis could still propagate to engagement or refills.
package hitl

import (
	"sort"
	"sync"
	"time"

	// medical is imported for the ReviewRequest type and the
	// ReviewStatus lifecycle constants. The queue is a pure container;
	// it does not interpret Kind or Payload contents.
	"github.com/PratikDhanave/bodh/pkg/medical"
	// uuid backs ReviewRequest.ID generation. The ID is the resume
	// handle the reviewer references when calling /reviews/{id}/decide.
	"github.com/google/uuid"
)

// Queue is a thread-safe in-memory store of ReviewRequests.
//
// In-memory is sufficient for the single-binary deployment. The production
// path is the same Postgres backend that backs the audit log (PR #10):
// queue persistence migrates to a Postgres table with the same tenant RLS
// predicate. The Queue API surface intentionally stays narrow (Submit /
// Decide / Get / List / Pending / Expire) so the swap is mechanical.
//
// IMPORTANT: this queue stores ReviewRequest.Payload, which can carry
// clinical detail intentionally (it is what the reviewer needs to see).
// That makes the queue a PHI surface. Its access path (HTTP /reviews
// handlers) must enforce reviewer authentication; the audit log does NOT
// store Payload — see pkg/audit and the FromReviewDecision helper.
type Queue struct {
	// mu is RWMutex because the read paths (Get, List, Pending,
	// CountByKind, CountByStatus) outnumber writes in practice — a UI
	// polling /reviews/pending every 5 seconds and a reviewer deciding
	// once per minute is the typical ratio.
	mu sync.RWMutex
	// requests is keyed by ReviewRequest.ID. A map (not a slice) because
	// Get / Decide are O(1) operations on the hot HTTP path; List does
	// the O(n) sort itself.
	requests map[string]medical.ReviewRequest
}

// NewQueue returns an empty queue.
func NewQueue() *Queue {
	return &Queue{requests: make(map[string]medical.ReviewRequest)}
}

// Submit stores a new request with status=pending and returns it (with id and
// SubmittedAt populated).
//
// ENVELOPE-METADATA PATTERN: the upstream agent wraps its payload in a
// ReviewRequest with NextTo / NextType set to the agent and message-type
// that should resume on approval (the envelope keys hitl_next_to /
// hitl_next_type from the originating message are copied onto these
// fields). The queue does not interpret the payload; it stores it
// verbatim. See docs/architecture.md § Envelope keys for the catalog.
//
// SLA semantics: SLAAt is set by the caller (computed from the upstream
// message's hitl_sla_minutes envelope key, default unset = no deadline).
// The queue does NOT auto-expire here; Expire() is called explicitly by a
// scheduler / cron / HTTP handler. This separation keeps Submit allocation-
// free and lets the operator control when expiration runs (e.g. once per
// minute, never on the hot path).
//
// PRIORITY semantics: r.Priority is informational ("low"/"normal"/"high"/
// "urgent") — the queue does not reorder by priority. Reviewers' UIs are
// expected to sort by priority client-side; the queue's only ordering
// guarantee is SubmittedAt ascending in List/Pending.
//
// Status is FORCED to ReviewPending regardless of what the caller passed.
// This prevents a buggy caller from submitting an already-decided review
// (which would skip the gate). The reviewer's terminal status only ever
// comes from Decide() or Expire().
func (q *Queue) Submit(r medical.ReviewRequest) medical.ReviewRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	if r.ID == "" {
		// New ID; UUIDv4 is sufficient — the queue does not use IDs as
		// security tokens (HTTP auth is the reviewer access control).
		r.ID = uuid.NewString()
	}
	if r.SubmittedAt.IsZero() {
		// UTC for consistency with the audit log timestamps; the explain
		// handler dedup logic in cmd/care/explain.go matches on .Equal,
		// which is UTC-aware but easier to reason about when both sides
		// agree on the zone.
		r.SubmittedAt = time.Now().UTC()
	}
	r.Status = medical.ReviewPending
	q.requests[r.ID] = r
	return r
}

// Decide records a reviewer's choice. decision must be one of
// "approve", "approve_with_modifications", "reject". Returns the updated
// request and true on success; (zero, false) if the id is unknown, already
// decided, or the decision string is invalid.
//
// LOSSLESS-ON-REJECT (the critical safety property):
// When decision == "reject", this method updates the record's Status to
// ReviewRejected and stamps DecidedAt + ReviewerID + Reason. The original
// Payload is preserved on the queue entry (still visible in List/Get) and
// the gate agent that originally submitted the request is responsible for
// emitting a KindReviewDecision audit event via audit.FromReviewDecision.
// The Payload is then DROPPED — the gate never republishes it onto the
// bus. This is the "no half-completed clinical action can leak past the
// gate" guarantee documented in docs/architecture.md § Lossless on reject.
//
// Reject is NOT destructive at the queue level: the rejection is part of
// the forensic record. The reviewer's rationale (Reason) is the human
// explanation captured for GDPR Article 22(3) "express his or her point
// of view and to contest the decision" — though here it's the clinician's
// reasoning for refusing to proceed, which serves the same audit purpose.
//
// IDEMPOTENCY: once Status != ReviewPending, this method short-circuits to
// (r, false). This makes a double-click on a reviewer UI "Approve" button
// safe (the second click returns false; the UI displays the first
// decision). It also blocks any attempt to flip a rejected review to
// approved retroactively — a desired property for the audit chain.
//
// The "default: return r, false" branch on an unknown decision string is
// the input-validation hook: HTTP callers can send arbitrary strings; we
// must not silently accept "deny" or "yes" as approval. The HTTP layer
// translates the boolean to 400 Bad Request.
func (q *Queue) Decide(id, reviewerID, decision, reason, modification string) (medical.ReviewRequest, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	r, ok := q.requests[id]
	if !ok || r.Status != medical.ReviewPending {
		// Unknown ID or already-decided: nothing to do. Returning false
		// is the signal the HTTP handler turns into 404 (unknown) or 409
		// (already decided) per the API contract.
		return r, false
	}
	switch decision {
	case "approve":
		r.Status = medical.ReviewApproved
	case "approve_with_modifications":
		// The reviewer accepted the action but the modification string
		// carries an edit that the resume path must apply. The gate
		// agent reads Modification and incorporates it before publishing
		// the resume message.
		r.Status = medical.ReviewModified
	case "reject":
		r.Status = medical.ReviewRejected
	default:
		return r, false
	}
	r.ReviewerID = reviewerID
	r.Reason = reason
	r.Modification = modification
	// UTC for the same reason Submit uses UTC; also: DecidedAt is the
	// audit-event timestamp the explain handler dedupes on.
	r.DecidedAt = time.Now().UTC()
	q.requests[id] = r
	return r, true
}

// Get returns a request by id. Returns (zero, false) if the id is unknown.
// The returned ReviewRequest is a copy by value — callers may not mutate it
// to affect the queue (Go map semantics). To update, call Decide.
func (q *Queue) Get(id string) (medical.ReviewRequest, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	r, ok := q.requests[id]
	return r, ok
}

// List returns every request sorted by SubmittedAt asc.
//
// Status lifecycle (lifecycle constants live in pkg/medical/review.go):
//
//	pending  ──Decide(approve)──────────────────► approved
//	  │      ──Decide(approve_with_modifications)► approved_with_modifications
//	  │      ──Decide(reject)─────────────────► rejected (lossless: payload retained,
//	  │                                                   audit event emitted, NEVER republished)
//	  └──Expire(now > SLAAt)────────────────────► expired (visible to reviewers,
//	                                                       never auto-resumes — see Expire below)
//
// Sort by SubmittedAt is the FIFO-ish default. Reviewer UIs that want a
// priority-weighted ordering re-sort the response client-side; the queue
// does not encode priority semantics (see Submit doc).
func (q *Queue) List() []medical.ReviewRequest {
	q.mu.RLock()
	defer q.mu.RUnlock()
	// Pre-size to len(map) to avoid append reallocations on busy queues.
	out := make([]medical.ReviewRequest, 0, len(q.requests))
	for _, r := range q.requests {
		out = append(out, r)
	}
	// Stable order is not required (no ties expected on SubmittedAt at
	// UTC nanosecond resolution); sort.Slice is sufficient.
	sort.Slice(out, func(i, j int) bool { return out[i].SubmittedAt.Before(out[j].SubmittedAt) })
	return out
}

// Pending returns only requests with status=pending, sorted by SubmittedAt asc.
//
// This is the primary endpoint reviewer UIs poll. Filtering happens after
// List() so we don't duplicate the sort logic; the cost is one extra
// allocation per call. Acceptable: this is not the hot path.
//
// NB: the slice trick `out := all[:0]` reuses the underlying array of the
// `all` slice — safe here because we hold no concurrent reference. If this
// is ever refactored to share `all` with a caller, replace with a fresh
// slice to avoid aliasing.
func (q *Queue) Pending() []medical.ReviewRequest {
	all := q.List()
	out := all[:0]
	for _, r := range all {
		if r.Status == medical.ReviewPending {
			out = append(out, r)
		}
	}
	return out
}

// CountByKind returns a histogram of all requests, grouped by Kind.
// Used by /metrics and operator dashboards to surface the review-type mix.
func (q *Queue) CountByKind() map[string]int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := map[string]int{}
	for _, r := range q.requests {
		out[r.Kind]++
	}
	return out
}

// CountByStatus returns a histogram grouped by Status.
// Used by /metrics to expose pending depth — the operational SLI for the
// gate's responsiveness. A growing `pending` count is the canary for
// "reviewers are overwhelmed" and triggers a paging alert in production.
func (q *Queue) CountByStatus() map[medical.ReviewStatus]int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := map[medical.ReviewStatus]int{}
	for _, r := range q.requests {
		out[r.Status]++
	}
	return out
}

// Expire scans pending requests and marks any past their SLA as expired.
// Returns how many were transitioned.
//
// EXPIRED REVIEWS STAY IN THE QUEUE. The Status flips to ReviewExpired and
// DecidedAt is stamped, but the row is NOT removed and the original
// payload is NOT republished. Expired entries are visible to reviewers
// (List() returns them) so the operator can see what was missed; the
// resume path explicitly checks Status == ReviewApproved (or Modified)
// and ignores anything else. This is a deliberate safety choice: a stale
// pending review for an action that may no longer be relevant (patient
// discharged, condition changed) must not auto-resume just because a
// scheduler ran. See docs/architecture.md § Human-in-the-loop pattern.
//
// `now` is a parameter (not time.Now()) so callers can advance a fake
// clock in tests and so a single sweep can use a consistent boundary
// across all entries (avoiding the rare case where two entries differ
// only because one was checked a microsecond later).
//
// SLAAt zero-value means "no deadline" — those entries are NEVER expired
// by this method. Production SLA defaults are applied at Submit time
// (envelope key hitl_sla_minutes); leaving SLAAt zero is the "review
// indefinitely" escape hatch for non-time-critical kinds.
func (q *Queue) Expire(now time.Time) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for id, r := range q.requests {
		// Skip anything already terminal — Decide may have raced ahead.
		if r.Status != medical.ReviewPending {
			continue
		}
		if !r.SLAAt.IsZero() && now.After(r.SLAAt) {
			r.Status = medical.ReviewExpired
			r.DecidedAt = now
			q.requests[id] = r
			n++
		}
	}
	return n
}
