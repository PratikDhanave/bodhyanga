package hitl

import (
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// resumeFrom is the From identity stamped on every resumed message. Using a
// fixed "human_review" (rather than impersonating the original upstream
// agent) keeps the audit trail honest: the resume is attributed to the HITL
// gate that produced it. Must match the human_review agent's bus ID.
const resumeFrom = "human_review"

// BuildResumeMessage turns a DECIDED review request into the message that
// resumes the paused pipeline. It is the pure, agent-free half of what the
// human_review agent's ApplyDecision used to do inline: the caller performs
// the ReviewStore.Decide() (which flips status under lock) and passes the
// resulting ReviewRequest here.
//
// It returns ok=false when the review did not approve — a rejected (or any
// non-approving terminal) review resumes nothing. On approve/modify it
// returns the message addressed to r.NextTo with the approved (or modified)
// payload and the HITL breadcrumb metadata.
//
// Why this lives in pkg/hitl and not in the agents package: the decoupled
// backend (cmd/api) applies HITL decisions over HTTP and must publish the
// resume message to the bus WITHOUT importing agent code. Both the monolith
// agent and the backend now share this one implementation, so their resume
// behavior cannot drift.
func BuildResumeMessage(r medical.ReviewRequest, reviewerID, modification string) (protocol.Message, bool) {
	// Only approved / modified reviews resume the pipeline. Reject (and any
	// other non-approving terminal status) yields no downstream message.
	if r.Status != medical.ReviewApproved && r.Status != medical.ReviewModified {
		return protocol.Message{}, false
	}
	// Pick the payload to republish: approve_with_modifications uses the
	// reviewer's edit; plain approve uses the original. An empty modification
	// on a modified review falls back to the original payload.
	payload := r.Payload
	if r.Status == medical.ReviewModified && modification != "" {
		payload = modification
	}
	// r.Meta already carries case_id / tenant_id / trace_id / patient_id (the
	// envelope keys were stripped when the request was parked). Add breadcrumb
	// keys so audit and downstream agents can correlate the resumed message
	// back to the review, and ensure no hitl_kind leaks onto the resumed
	// message (downstream agents key off its absence).
	meta := copyMeta(r.Meta)
	meta["hitl_review_id"] = r.ID
	meta["hitl_reviewer_id"] = reviewerID
	delete(meta, "hitl_kind")
	return protocol.NewMessage(resumeFrom, r.NextTo, protocol.RoleAgent, r.NextType, payload, meta), true
}

// copyMeta returns a shallow copy of m, never nil, so callers can write
// breadcrumb keys without mutating the source review's metadata.
func copyMeta(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
