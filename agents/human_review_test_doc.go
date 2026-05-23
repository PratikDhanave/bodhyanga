// human_review_test.go — unit tests for the HITL gate agent.
//
// Overview:
//   Covers envelope parsing into the review queue, approve/resume routing
//   with audit breadcrumbs, reject emitting nothing, and the drop behaviour
//   when envelope keys are missing.
//
// Key declarations:
//   - envelopedMsg: helper building a review_request with full HITL envelope.
//   - TestHumanReviewQueuesPending: queue submit + envelope strip.
//   - TestHumanReviewApproveResumes: approval republish path.
//   - TestHumanReviewRejectEmitsNothing: rejection silent path.
//   - TestHumanReviewMissingEnvelopeDrops: defensive drop on bad envelope.
//
// See doc.go for package-level documentation.

package agents
