// panel.go — population-health panel rules and aggregate gap reports.
//
// Overview:
//   Provides the demo rule set for the panel manager: A1c overdue/uncontrolled,
//   uncontrolled BP, annual-visit due, CHF weight trend, and post-discharge
//   transitional-care follow-up. BuildPanelReport runs every rule across
//   every patient and rolls up condition counts plus care gaps.
//
// Key declarations:
//   - CareGap: single open quality/preventive-care issue
//   - PanelReport: aggregate panel-wide output with gaps and condition roll
//   - PanelRule: function type evaluating one PatientRecord
//   - DefaultPanelRules: built-in demo rule set
//   - BuildPanelReport: runs rules across a patient panel
//
// See doc.go for package-level documentation.

package medical
