// careplan.go — condition-specific care plan construction and serialization.
//
// Overview:
//   Defines the CarePlan structure emitted by the chronic-disease-management
//   (CDM) agent along with deterministic builders for common conditions
//   (HTN, DM, CHF, CKD, COPD). Provides JSON encoding helpers so plans can
//   travel across the agent message bus.
//
// Key declarations:
//   - CarePlan: condition-specific plan with goals, medications, and monitoring
//   - GoalEntry, MedicationEntry, MonitoringEntry: building blocks of a CarePlan
//   - BuildCarePlan: deterministic per-condition plan factory
//   - EncodeCarePlan / DecodeCarePlan: JSON round-trip helpers
//   - CarePlan.Summary: one-line summary for logs/UI
//
// See doc.go for package-level documentation.

package medical
