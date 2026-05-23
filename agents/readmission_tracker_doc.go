// readmission_tracker.go — ADT event ingest with readmission risk scoring.
//
// Overview:
//   Consumes ADT events, updates the patient registry, scores readmission
//   risk, and on discharge emits a discharge_event (optionally HITL-gated)
//   that hands off to the TCM coordinator.
//
// Key declarations:
//   - ReadmissionTrackerAgent: agent with Registry and HITL flag.
//   - ReadmissionTrackerAgent.HandleMessage: processes ADT and emits risk + discharge.
//
// See doc.go for package-level documentation.

package agents
