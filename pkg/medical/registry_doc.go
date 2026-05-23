// registry.go — thread-safe in-memory patient roster used by the panel manager.
//
// Overview:
//   Holds a population-health snapshot of patients (conditions, recent labs,
//   vitals, RPM enrollment, discharge dates) and exposes basic upsert/query
//   operations. The panel manager iterates this registry to drive care-gap
//   rules and outreach.
//
// Key declarations:
//   - PatientRecord: demo-grade per-patient snapshot (no PHI)
//   - PatientRegistry: thread-safe map of PatientID -> PatientRecord
//   - NewPatientRegistry: constructor for an empty registry
//   - Upsert / Get / List / FilterByCondition: registry operations
//
// See doc.go for package-level documentation.

package medical
