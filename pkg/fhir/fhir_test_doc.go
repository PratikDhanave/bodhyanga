// fhir_test.go — unit tests for the FHIR R4 adapter helpers.
//
// Overview:
//   Validates that an Observation with a recognized vital sign code is mapped
//   to an RPM observation with the right metric and value, that a finished
//   Encounter becomes a discharge ADT event with the correct end time, and
//   that a Patient with a birth date is folded into a PatientRecord with a
//   sensible computed age.
//
// Key declarations:
//   - TestObservationToRPM: maps a systolic-BP Observation to an RPMObservation.
//   - TestEncounterToADTDischarge: maps a finished Encounter to a discharge event.
//   - TestPatientToRecord: confirms Patient -> PatientRecord field mapping and age math.
//
// See doc.go for package-level documentation.

package fhir
