// main.go — in-process exerciser for the care-services workflows.
//
// Overview:
//   Seeds patients, registers the care-services agents with HITL on by
//   default, then publishes representative messages for CDM, TCM, panel,
//   virtual office (inbox / refills / prior-auth), and ADT flows. A drain
//   loop auto-approves any queued reviews so the demo runs end-to-end.
//
// Key declarations:
//   - main: wires up agents and runs the selected -workflow.
//   - seedPatients: seeds a three-patient demo registry.
//   - runCDM / runTCM / runPanel / runVirtualOffice / runADT: workflow drivers.
//
// See doc.go for package-level documentation.

package main
