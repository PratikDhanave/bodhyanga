// fhir.go — minimal FHIR R4 adapter for the care-services demo flows.
//
// Overview:
//   Provides slim Patient / Observation / Encounter / Condition structs and
//   conversion helpers that fold FHIR resources into pkg/medical types
//   (PatientRecord, RPMObservation, ADTEvent) and export CarePlan goals back
//   out as Observations. Only the fields actually exercised in the demo are
//   modeled — this is not a full FHIR implementation.
//
// Key declarations:
//   - Patient, Observation, Encounter, Condition: slim FHIR R4 resource structs.
//   - CodeableConcept, Coding, Reference, Quantity, Period: shared sub-resources.
//   - PatientToRecord, ObservationToRPM, EncounterToADT, ConditionCode: import helpers.
//   - CarePlanToObservations: export helper turning plan goals into Observations.
//   - Marshal: convenience JSON marshaller for FHIR responses.
//
// See doc.go for package-level documentation.

package fhir
