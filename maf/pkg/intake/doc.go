// Package intake is the front door that translates inbound clinical messages
// (native JSON, HL7 v2 ADT, FHIR R4 bundles) into the pipeline's PatientCase
// (specs/002). It exists so the unchanged MARA graph can ingest real hospital
// protocols without any agent below intake knowing the wire format.
//
// Contract:
//
//   - Import-unchanged: HL7 parsing (bodhyanga/pkg/hl7v2) and FHIR resource types
//     (bodhyanga/pkg/fhir) are imported as-is via the module's replace ../bodhyanga. Only
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
// Dependency note (T001): `go list -deps github.com/PratikDhanave/bodhyanga/pkg/hl7v2
// github.com/PratikDhanave/bodhyanga/pkg/fhir` resolves through the existing
// `replace ../bodhyanga` (go.mod) with no new require — hl7v2 pulls only bodhyanga/pkg/llm
// and bodhyanga/pkg/medical, both already in the module graph.
package intake
