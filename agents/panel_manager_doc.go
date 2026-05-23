// panel_manager.go — population-health rules engine for the patient roster.
//
// Overview:
//   Runs care-gap rules over the patient registry on panel_request, emits a
//   panel_report, and produces per-gap engagement_request messages routed to
//   the engagement agent (or to human_review when HITL is enabled).
//
// Key declarations:
//   - PanelManagerAgent: agent with Registry, Rules, and HITL flag.
//   - PanelManagerAgent.HandleMessage: dispatches care_plan_ready or panel_request.
//   - priorityFromSeverity: maps gap severity to HITL priority bucket.
//
// See doc.go for package-level documentation.

package agents
