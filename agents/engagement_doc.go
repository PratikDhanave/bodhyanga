// engagement.go — patient outreach via SMS notifier for several upstream agents.
//
// Overview:
//   Fans in care_plan_ready, engagement_request, tcm_tasks_created, and
//   refill_response messages, formats a patient-facing body, sends through
//   the engagement.Notifier, and emits an engagement_sent record.
//
// Key declarations:
//   - EngagementAgent: agent with engagement.Notifier dependency.
//   - EngagementAgent.HandleMessage: dispatches per upstream message type.
//   - summaryGoals: formats the first care plan goal for the SMS body.
//
// See doc.go for package-level documentation.

package agents
