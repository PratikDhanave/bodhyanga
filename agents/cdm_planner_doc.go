// cdm_planner.go — chronic disease management care plan generator.
//
// Overview:
//   Builds a condition-specific care plan (HTN, DM, CHF, CKD, COPD), updates
//   the patient registry with the new medications and conditions, and emits
//   care_plan_ready to engagement (optionally HITL-gated) and to panel_manager
//   for tracking.
//
// Key declarations:
//   - CDMPlannerAgent: agent with Registry and HITL flag.
//   - CDMPlannerAgent.HandleMessage: generates plan and emits both routes.
//   - appendIfMissing: dedupes a condition into the registry list.
//
// See doc.go for package-level documentation.

package agents
