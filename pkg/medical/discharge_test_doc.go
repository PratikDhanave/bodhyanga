// discharge_test.go — tests for medication reconciliation logic.
//
// Overview:
//   Drives ReconcileMedications with overlapping home and current med lists
//   and asserts the resulting added/stopped/kept partitions plus the
//   duplicate-class conflict detection (two loop diuretics).
//
// Key declarations:
//   - TestReconcileMedications: covers kept/stopped/added partitions and conflicts
//
// See doc.go for package-level documentation.

package medical
