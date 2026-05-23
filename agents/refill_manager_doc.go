// refill_manager.go — medication refill decision with registry lookup.
//
// Overview:
//   Auto-approves refills when the patient's active care plan already has
//   the medication; otherwise queues a nurse review task. Patient-facing
//   refill_response messages are optionally routed through human_review.
//
// Key declarations:
//   - RefillManagerAgent: agent with Tasks queue, Registry, and HITL flag.
//   - RefillManagerAgent.HandleMessage: decides and routes the refill response.
//
// See doc.go for package-level documentation.

package agents
