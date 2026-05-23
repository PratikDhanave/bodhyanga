// human_review.go — human-in-the-loop (HITL) gate for clinically consequential actions.
//
// Overview:
//   Receives review_request messages wrapped in an HITL envelope (hitl_kind,
//   hitl_next_to, hitl_next_type, etc.), queues them for clinician decision,
//   and on ApplyDecision resumes approved payloads onto the bus toward the
//   originally intended recipient with envelope keys stripped.
//
// Key declarations:
//   - HumanReviewAgent: agent owning the review hitl.Queue.
//   - HumanReviewAgent.HandleMessage: parses envelope and queues the request.
//   - HumanReviewAgent.ApplyDecision: records decision and returns resume messages.
//   - reviewOptions / reviewEnvelope: helpers used by upstream agents to wrap a message.
//   - envelopeKeys: metadata keys stripped before republishing.
//
// See doc.go for package-level documentation.

package agents
