// fhir_query.go — FHIRQuery stub Tool.
//
// Overview:
//   Returns deterministic FHIR-style Patient records from a small in-memory
//   map (eight synthetic patients spanning common chronic conditions). The
//   Tool does NOT talk to a real FHIR server — every output is tagged
//   metadata.source = "demo-synth" so downstream agents know the data is
//   illustrative. On a miss the response is still well-formed (found=false)
//   so the LLM can decide how to proceed.
//
// Key declarations:
//   - FHIRQuery: zero-value Tool implementation
//   - fhirPatients: the synthetic patient table (package-private)
//   - fhirPatient / fhirMiss: hit / miss output shapes
//
// See doc.go for package-level documentation.

package tools
