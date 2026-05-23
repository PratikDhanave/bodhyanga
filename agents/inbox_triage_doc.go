// inbox_triage.go — classifier and router for inbound patient messages.
//
// Overview:
//   Classifies inbox messages by subject and body keywords (refill,
//   prior_auth, urgent_clinical, general) and either auto-routes to the
//   matching downstream agent or queues a nurse task. Emits inbox_routed.
//
// Key declarations:
//   - InboxTriageAgent: agent with Tasks queue dependency.
//   - InboxTriageAgent.HandleMessage: classifies and routes the message.
//   - classifyInbox / extractMedication / extractCode: keyword helpers.
//
// See doc.go for package-level documentation.

package agents
