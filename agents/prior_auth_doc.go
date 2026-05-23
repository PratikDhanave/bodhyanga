// prior_auth.go — payer prior authorization request handler with optional HITL gating.
//
// Overview:
//   Consumes prior_auth_request messages, decides auto-approval for low-risk
//   imaging codes (DEXA, CT), and otherwise routes to nurse review or through
//   the human_review gate. Emits prior_auth_status messages downstream.
//
// Key declarations:
//   - PriorAuthAgent: agent struct with Tasks queue and HITL flag.
//   - PriorAuthAgent.HandleMessage: classifies and routes the PA request.
//   - lowRiskAuthCodes: map of codes eligible for automatic approval.
//
// See doc.go for package-level documentation.

package agents
