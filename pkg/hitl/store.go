package hitl

import "github.com/PratikDhanave/bodh/pkg/medical"

// ReviewStore is the human-in-the-loop review surface shared by the
// human_review agent and the backend HTTP layer. It is the union of the
// *Queue methods those two consumers call:
//
//	Submit / Decide      — human_review agent (park a request; apply a decision)
//	Get / List / Pending — backend read endpoints (review detail, list, inbox)
//
// Why this exists (agent decoupling, Phase 0): the human_review agent and the
// API handlers both bind to the concrete in-memory *Queue today. Depending on
// this interface lets a later step back reviews with Postgres (same
// signatures) so a separately deployed human_review container and the backend
// share review state through the database instead of a shared process.
//
// Kept NARROW: CountByKind / CountByStatus / Expire live on *Queue but are
// not part of any consumer's critical path, so they stay off the interface
// until a consumer needs them.
type ReviewStore interface {
	Submit(r medical.ReviewRequest) medical.ReviewRequest
	Decide(id, reviewerID, decision, reason, modification string) (medical.ReviewRequest, bool)
	Get(id string) (medical.ReviewRequest, bool)
	List() []medical.ReviewRequest
	Pending() []medical.ReviewRequest
}

// Compile-time assertion that the in-memory queue satisfies the interface.
var _ ReviewStore = (*Queue)(nil)
