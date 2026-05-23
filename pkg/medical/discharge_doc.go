// discharge.go — discharge summary payload and medication reconciliation.
//
// Overview:
//   Defines the transitional-care-management (TCM) discharge summary and a
//   simple medication-reconciliation routine that compares home and current
//   med lists, flagging additions, stops, kept entries, and duplicate-class
//   conflicts.
//
// Key declarations:
//   - DischargeSummary: TCM discharge payload (meds, follow-up days, risk flag)
//   - MedReconciliation: added/stopped/kept/conflict deltas between med lists
//   - EncodeDischarge / DecodeDischarge: JSON helpers for bus transport
//   - ReconcileMedications: compares two med lists and flags class conflicts
//
// See doc.go for package-level documentation.

package medical
