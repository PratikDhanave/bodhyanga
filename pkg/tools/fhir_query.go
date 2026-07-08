package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// FHIRQuery is a stub Tool that returns deterministic patient records from
// a small in-memory map. It mimics the shape of a FHIR Patient resource —
// id, name, dob, conditions, medications — without talking to a real FHIR
// server. The data is illustrative and tagged metadata.source = "demo-synth"
// so consumers do not mistake it for production data.
//
// Input shape (JSON):
//
//	{"resource":"Patient","patient_id":"pt-001"}
//
// Output shape (JSON, on hit):
//
//	{
//	  "resource":"Patient",
//	  "id":"pt-001",
//	  "name":"Jane Doe",
//	  "dob":"1968-03-12",
//	  "conditions":["essential hypertension","type 2 diabetes"],
//	  "medications":["lisinopril","metformin"],
//	  "found":true,
//	  "metadata":{"source":"demo-synth"}
//	}
//
// Output shape (JSON, on miss):
//
//	{"resource":"Patient","id":"<requested>","found":false,
//	 "metadata":{"source":"demo-synth"}}
type FHIRQuery struct{}

// Name implements Tool.
func (FHIRQuery) Name() string { return "fhir_query" }

// Description implements Tool.
func (FHIRQuery) Description() string {
	return "Stub FHIR Patient lookup. Returns demographics, problem list, and active medications for a known patient ID. Demo data only (8 synthetic patients), tagged source=demo-synth. Not a real FHIR client."
}

// Schema implements Tool. Resource is fixed to "Patient" for this slice; we
// keep the field on the input so future expansion (Observation, Condition,
// MedicationStatement) does not require a schema break.
func (FHIRQuery) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "resource": {
      "type": "string",
      "enum": ["Patient"],
      "description": "FHIR resource type. Only Patient is supported in this stub."
    },
    "patient_id": {
      "type": "string",
      "description": "Patient identifier, e.g. \"pt-001\"."
    }
  },
  "required": ["resource", "patient_id"],
  "additionalProperties": false
}`)
}

// fhirInput mirrors the JSON shape Invoke accepts.
type fhirInput struct {
	Resource  string `json:"resource"`
	PatientID string `json:"patient_id"`
}

// fhirPatient is the synthetic record shape returned on a hit.
type fhirPatient struct {
	Resource    string         `json:"resource"`
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	DOB         string         `json:"dob"`
	Sex         string         `json:"sex"`
	Conditions  []string       `json:"conditions"`
	Medications []string       `json:"medications"`
	Allergies   []string       `json:"allergies,omitempty"`
	Found       bool           `json:"found"`
	Metadata    map[string]any `json:"metadata"`
}

// fhirMiss is the shape returned when the patient is not in the table.
type fhirMiss struct {
	Resource string         `json:"resource"`
	ID       string         `json:"id"`
	Found    bool           `json:"found"`
	Metadata map[string]any `json:"metadata"`
}

// fhirPatients is the synthetic patient table. Eight entries cover a spread
// of common chronic conditions; the data is illustrative only.
var fhirPatients = map[string]fhirPatient{
	"pt-001": {
		Resource: "Patient", ID: "pt-001", Name: "Jane Doe", DOB: "1968-03-12", Sex: "female",
		Conditions:  []string{"essential hypertension", "type 2 diabetes"},
		Medications: []string{"lisinopril", "metformin"},
		Allergies:   []string{"penicillin"},
	},
	"pt-002": {
		Resource: "Patient", ID: "pt-002", Name: "John Smith", DOB: "1955-11-04", Sex: "male",
		Conditions:  []string{"atrial fibrillation", "coronary artery disease"},
		Medications: []string{"warfarin", "metoprolol", "atorvastatin"},
	},
	"pt-003": {
		Resource: "Patient", ID: "pt-003", Name: "Maria Garcia", DOB: "1972-07-22", Sex: "female",
		Conditions:  []string{"asthma", "seasonal allergic rhinitis"},
		Medications: []string{"albuterol", "fluticasone"},
	},
	"pt-004": {
		Resource: "Patient", ID: "pt-004", Name: "Hiroshi Tanaka", DOB: "1948-01-30", Sex: "male",
		Conditions:  []string{"chronic kidney disease stage 3", "gout"},
		Medications: []string{"allopurinol", "amlodipine"},
	},
	"pt-005": {
		Resource: "Patient", ID: "pt-005", Name: "Aisha Khan", DOB: "1990-09-15", Sex: "female",
		Conditions:  []string{"migraine"},
		Medications: []string{"sumatriptan"},
	},
	"pt-006": {
		Resource: "Patient", ID: "pt-006", Name: "Robert Chen", DOB: "1962-04-08", Sex: "male",
		Conditions:  []string{"congestive heart failure", "type 2 diabetes"},
		Medications: []string{"furosemide", "carvedilol", "spironolactone", "metformin"},
	},
	"pt-007": {
		Resource: "Patient", ID: "pt-007", Name: "Elena Rossi", DOB: "1985-12-19", Sex: "female",
		Conditions:  []string{"hypothyroidism"},
		Medications: []string{"levothyroxine"},
	},
	"pt-008": {
		Resource: "Patient", ID: "pt-008", Name: "Samuel Okafor", DOB: "1979-06-03", Sex: "male",
		Conditions:  []string{"depression", "GERD"},
		Medications: []string{"sertraline", "omeprazole"},
	},
}

// Invoke implements Tool. Looks up the patient in the synthetic table and
// returns either the full record (Found=true) or a miss stub (Found=false).
// Unknown resource types are rejected with an error so the caller can fall
// back to a different tool.
func (FHIRQuery) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("fhir_query: empty arguments")
	}
	var in fhirInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("fhir_query: decode arguments: %w", err)
	}
	resource := strings.TrimSpace(in.Resource)
	if resource == "" {
		resource = "Patient"
	}
	if resource != "Patient" {
		return nil, fmt.Errorf("fhir_query: unsupported resource %q (only Patient)", in.Resource)
	}
	id := strings.TrimSpace(in.PatientID)
	if id == "" {
		return nil, fmt.Errorf("fhir_query: empty patient_id")
	}

	if p, ok := fhirPatients[strings.ToLower(id)]; ok {
		p.Found = true
		p.Metadata = map[string]any{"source": "demo-synth"}
		return json.Marshal(p)
	}
	miss := fhirMiss{
		Resource: "Patient",
		ID:       id,
		Found:    false,
		Metadata: map[string]any{"source": "demo-synth"},
	}
	return json.Marshal(miss)
}
