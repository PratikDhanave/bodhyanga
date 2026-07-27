package intake

import (
	"encoding/json"

	"github.com/PratikDhanave/bodh/pkg/fhir"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// bundle is the minimal FHIR R4 Bundle envelope. bodh/pkg/fhir has no Bundle
// type (it deals in individual resources), so this thin decoder is the only new
// parser in the feature: it splits a bundle into resources and hands each to
// bodh's resource types (imported unchanged).
type bundle struct {
	ResourceType string `json:"resourceType"`
	Entry        []struct {
		Resource json.RawMessage `json:"resource"`
	} `json:"entry"`
}

// resourcePeek reads just the discriminator so we can route each entry.
type resourcePeek struct {
	ResourceType string `json:"resourceType"`
}

// translateFHIR turns a FHIR R4 bundle into one PatientCase. Exactly one
// Patient is required (zero → missing_resource, many → ambiguous_subject).
// Conditions seed History and recognized Observations seed Symptoms; a bundle
// with a Patient but no clinical resources is undifferentiated, not an error
// (spec FR-002/FR-008).
func translateFHIR(payload []byte) (agents.PatientCase, error) {
	var b bundle
	if err := json.Unmarshal(payload, &b); err != nil {
		return agents.PatientCase{}, reject(ReasonMalformed)
	}

	var (
		patients   []fhir.Patient
		conditions []fhir.Condition
		observed   []fhir.Observation
	)
	for _, e := range b.Entry {
		var peek resourcePeek
		if err := json.Unmarshal(e.Resource, &peek); err != nil {
			return agents.PatientCase{}, reject(ReasonMalformed)
		}
		switch peek.ResourceType {
		case "Patient":
			var p fhir.Patient
			if err := json.Unmarshal(e.Resource, &p); err != nil {
				return agents.PatientCase{}, reject(ReasonMalformed)
			}
			patients = append(patients, p)
		case "Condition":
			var c fhir.Condition
			if err := json.Unmarshal(e.Resource, &c); err != nil {
				return agents.PatientCase{}, reject(ReasonMalformed)
			}
			conditions = append(conditions, c)
		case "Observation":
			var o fhir.Observation
			if err := json.Unmarshal(e.Resource, &o); err != nil {
				return agents.PatientCase{}, reject(ReasonMalformed)
			}
			observed = append(observed, o)
		}
	}

	switch len(patients) {
	case 1: // ok
	case 0:
		return agents.PatientCase{}, reject(ReasonMissingResource)
	default:
		return agents.PatientCase{}, reject(ReasonAmbiguousSubject)
	}

	pc := agents.PatientCase{CaseID: patients[0].ID}
	for _, c := range conditions {
		if code := fhir.ConditionCode(c); code != "" {
			pc.History = append(pc.History, code)
		}
	}
	for _, o := range observed {
		if rpm, ok := fhir.ObservationToRPM(o); ok && rpm.Metric != "" {
			pc.Symptoms = append(pc.Symptoms, rpm.Metric)
		}
	}
	return pc, nil
}
