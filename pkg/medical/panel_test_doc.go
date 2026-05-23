// panel_test.go — tests for the default panel rule set.
//
// Overview:
//   Builds a small synthetic patient panel covering diabetic, hypertensive,
//   CHF, and overdue-visit profiles, then asserts that the expected rule
//   names (a1c_uncontrolled, bp_uncontrolled, chf_weight_alert,
//   annual_visit_due, tcm_followup_open) all fire in the panel report.
//
// Key declarations:
//   - TestDefaultPanelRules: end-to-end check of BuildPanelReport + DefaultPanelRules
//
// See doc.go for package-level documentation.

package medical
