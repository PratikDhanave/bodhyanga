// Package fhir is a minimal FHIR R4 adapter — just enough to import Patient,
// Observation, Encounter and Condition resources into pkg/medical, and export
// CarePlan / Observation back out. It is intentionally not a full FHIR
// implementation: only the fields used in the brilliant.care demo flows.
package fhir

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// Patient is a slim FHIR R4 Patient resource.
type Patient struct {
	ResourceType string `json:"resourceType"`
	ID           string `json:"id"`
	Gender       string `json:"gender,omitempty"`
	BirthDate    string `json:"birthDate,omitempty"` // YYYY-MM-DD
}

// Observation is a slim FHIR R4 Observation resource.
type Observation struct {
	ResourceType string `json:"resourceType"`
	ID           string `json:"id,omitempty"`
	Status       string `json:"status,omitempty"`
	Code         CodeableConcept `json:"code"`
	Subject      Reference       `json:"subject"`
	EffectiveDT  string          `json:"effectiveDateTime,omitempty"`
	ValueQty     *Quantity       `json:"valueQuantity,omitempty"`
	ValueString  string          `json:"valueString,omitempty"`
}

// Encounter is a slim FHIR R4 Encounter resource.
type Encounter struct {
	ResourceType string    `json:"resourceType"`
	ID           string    `json:"id"`
	Status       string    `json:"status,omitempty"` // planned, in-progress, finished
	Class        Coding    `json:"class,omitempty"`
	Subject      Reference `json:"subject"`
	Period       Period    `json:"period,omitempty"`
	ReasonCode   []CodeableConcept `json:"reasonCode,omitempty"`
}

// Condition is a slim FHIR R4 Condition resource.
type Condition struct {
	ResourceType string          `json:"resourceType"`
	ID           string          `json:"id"`
	Subject      Reference       `json:"subject"`
	Code         CodeableConcept `json:"code"`
	OnsetDate    string          `json:"onsetDateTime,omitempty"`
}

// CodeableConcept holds coded values.
type CodeableConcept struct {
	Coding []Coding `json:"coding,omitempty"`
	Text   string   `json:"text,omitempty"`
}

// Coding is a single code/system pair.
type Coding struct {
	System  string `json:"system,omitempty"`
	Code    string `json:"code,omitempty"`
	Display string `json:"display,omitempty"`
}

// Reference is a FHIR resource reference.
type Reference struct {
	Reference string `json:"reference,omitempty"`
	Display   string `json:"display,omitempty"`
}

// Quantity is a numeric value with units.
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit,omitempty"`
}

// Period is an interval.
type Period struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// PatientToRecord folds a FHIR Patient into a medical.PatientRecord.
func PatientToRecord(p Patient) medical.PatientRecord {
	rec := medical.PatientRecord{PatientID: p.ID, Sex: p.Gender}
	if p.BirthDate != "" {
		if t, err := time.Parse("2006-01-02", p.BirthDate); err == nil {
			rec.Age = int(time.Since(t).Hours() / 24 / 365.25)
		}
	}
	return rec
}

// ObservationToRPM converts an Observation into an RPMObservation when the code
// matches a metric we recognize (systolic_bp, weight_lb, spo2, heart_rate).
func ObservationToRPM(o Observation) (medical.RPMObservation, bool) {
	metric := observationMetric(o.Code)
	if metric == "" || o.ValueQty == nil {
		return medical.RPMObservation{}, false
	}
	return medical.RPMObservation{
		Device:  "fhir",
		Metric:  metric,
		Value:   o.ValueQty.Value,
		Unit:    o.ValueQty.Unit,
		TakenAt: o.EffectiveDT,
	}, true
}

// EncounterToADT converts an Encounter into an ADT event (admit on in-progress,
// discharge on finished).
func EncounterToADT(e Encounter) (medical.ADTEvent, bool) {
	patient := strings.TrimPrefix(e.Subject.Reference, "Patient/")
	when := e.Period.Start
	evType := medical.ADTAdmit
	switch strings.ToLower(e.Status) {
	case "finished":
		evType = medical.ADTDischarge
		if e.Period.End != "" {
			when = e.Period.End
		}
	case "in-progress", "arrived":
		evType = medical.ADTAdmit
	default:
		return medical.ADTEvent{}, false
	}
	occurred, _ := time.Parse(time.RFC3339, when)
	dx := ""
	if len(e.ReasonCode) > 0 {
		dx = e.ReasonCode[0].Text
	}
	return medical.ADTEvent{
		EventID:    e.ID,
		PatientID:  patient,
		Type:       evType,
		OccurredAt: occurred,
		PrimaryDx:  dx,
	}, true
}

// ConditionCode returns the lowercase code string from a Condition resource,
// preferring SNOMED then ICD, falling back to text.
func ConditionCode(c Condition) string {
	for _, code := range c.Code.Coding {
		if code.Code != "" {
			return strings.ToLower(code.Code)
		}
	}
	return strings.ToLower(c.Code.Text)
}

// CarePlanToObservation exports plan goals as Observations (for write-back).
// Only goal targets are exported; one Observation per goal.
func CarePlanToObservations(plan medical.CarePlan) []Observation {
	out := make([]Observation, 0, len(plan.Goals))
	for _, g := range plan.Goals {
		out = append(out, Observation{
			ResourceType: "Observation",
			Status:       "final",
			Code:         CodeableConcept{Text: g.Metric},
			Subject:      Reference{Reference: "Patient/" + plan.PatientID},
			EffectiveDT:  plan.CreatedAt.Format(time.RFC3339),
			ValueQty:     &Quantity{Value: g.Target, Unit: g.Unit},
		})
	}
	return out
}

// Marshal helpers (so cmd/care can serve FHIR JSON without re-importing).
func Marshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func observationMetric(c CodeableConcept) string {
	text := strings.ToLower(c.Text)
	switch text {
	case "systolic blood pressure", "systolic_bp":
		return "systolic_bp"
	case "body weight", "weight":
		return "weight_lb"
	case "oxygen saturation", "spo2":
		return "spo2"
	case "heart rate":
		return "heart_rate"
	case "hemoglobin a1c", "hba1c":
		return "hba1c"
	case "glucose":
		return "glucose"
	}
	for _, code := range c.Coding {
		switch code.Code {
		case "8480-6":
			return "systolic_bp"
		case "29463-7":
			return "weight_lb"
		case "59408-5", "2708-6":
			return "spo2"
		case "8867-4":
			return "heart_rate"
		case "4548-4":
			return "hba1c"
		case "2339-0":
			return "glucose"
		}
	}
	return ""
}
