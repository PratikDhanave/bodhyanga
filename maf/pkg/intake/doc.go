// Package intake is the front door that translates inbound clinical messages
// (native JSON, HL7 v2 ADT, FHIR R4 bundles) into the pipeline's PatientCase
// (specs/002). It exists so the unchanged MARA graph can ingest real hospital
// protocols without any agent below intake knowing the wire format.
//
// Contract:
//
//   - Import-unchanged: HL7 parsing (bodh/pkg/hl7v2) and FHIR resource types
//     (bodh/pkg/fhir) are imported as-is via the module's replace ../bodh. Only
//     the medical.*→agents.PatientCase mapping and a minimal FHIR Bundle
//     envelope are new code here.
//   - Dependency direction is one-way: intake → agents. The Inbound/Format
//     message types live in pkg/agents (with PatientCase); Translate is injected
//     into agents.NewTranslator, so pkg/agents never imports pkg/intake.
//   - PHI-free rejection taxonomy: every un-translatable input yields a
//     *RejectError whose Error() is drawn only from a RejectReason category
//     (plus an optional hashed fingerprint) — never message content.
//   - The medical.*→PatientCase mapping is demo-grade: an ADT/FHIR feed carries
//     identifiers and diagnoses, not a symptom workup, so undifferentiated
//     cases are an expected, designed outcome (FR-008), not an error.
//
// Dependency note (T001): `go list -deps github.com/PratikDhanave/bodh/pkg/hl7v2
// github.com/PratikDhanave/bodh/pkg/fhir` resolves through the existing
// `replace ../bodh` (go.mod) with no new require — hl7v2 pulls only bodh/pkg/llm
// and bodh/pkg/medical, both already in the module graph.
package intake
