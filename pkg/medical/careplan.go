package medical

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CarePlan is a condition-specific plan emitted by the CDM agent.
type CarePlan struct {
	CaseID      string            `json:"case_id"`
	PatientID   string            `json:"patient_id,omitempty"`
	Condition   string            `json:"condition"`
	Goals       []GoalEntry       `json:"goals,omitempty"`
	Medications []MedicationEntry `json:"medications,omitempty"`
	Monitoring  []MonitoringEntry `json:"monitoring,omitempty"`
	Education   []string          `json:"education,omitempty"`
	NextReview  time.Time         `json:"next_review"`
	CreatedAt   time.Time         `json:"created_at"`
}

// GoalEntry is a single SMART-style goal in a CarePlan.
type GoalEntry struct {
	Metric string  `json:"metric"`
	Target float64 `json:"target"`
	Unit   string  `json:"unit"`
	Window string  `json:"window,omitempty"` // e.g. "90d"
}

// MedicationEntry records a planned or current medication.
type MedicationEntry struct {
	Name      string `json:"name"`
	Dose      string `json:"dose,omitempty"`
	Frequency string `json:"frequency,omitempty"`
	Class     string `json:"class,omitempty"`
}

// MonitoringEntry says what device/test to track and how often.
type MonitoringEntry struct {
	Metric       string  `json:"metric"`
	Cadence      string  `json:"cadence"` // "daily" / "weekly" / "monthly"
	Source       string  `json:"source"`  // "rpm" / "lab" / "self_report"
	ThresholdMin float64 `json:"threshold_min,omitempty"`
	ThresholdMax float64 `json:"threshold_max,omitempty"`
}

// BuildCarePlan returns a deterministic care plan for a known condition.
// Reason: care plans must be auditable and reproducible for a research demo.
func BuildCarePlan(caseID, patientID, condition string, now time.Time) CarePlan {
	plan := CarePlan{
		CaseID:    caseID,
		PatientID: patientID,
		Condition: strings.ToLower(condition),
		CreatedAt: now.UTC(),
	}
	switch plan.Condition {
	case "htn", "hypertension":
		plan.Goals = []GoalEntry{{Metric: "systolic_bp", Target: 130, Unit: "mmHg", Window: "90d"}}
		plan.Medications = []MedicationEntry{{Name: "Lisinopril", Dose: "10 mg", Frequency: "daily", Class: "ACE inhibitor"}}
		plan.Monitoring = []MonitoringEntry{{Metric: "systolic_bp", Cadence: "daily", Source: "rpm", ThresholdMax: 160}}
		plan.Education = []string{"Low-sodium diet", "Daily home BP log"}
		plan.NextReview = now.AddDate(0, 1, 0)
	case "dm", "diabetes":
		plan.Goals = []GoalEntry{{Metric: "hba1c", Target: 7.0, Unit: "%", Window: "90d"}}
		plan.Medications = []MedicationEntry{{Name: "Metformin", Dose: "500 mg", Frequency: "BID", Class: "Biguanide"}}
		plan.Monitoring = []MonitoringEntry{{Metric: "glucose", Cadence: "daily", Source: "rpm", ThresholdMin: 70, ThresholdMax: 180}}
		plan.Education = []string{"Carb counting", "Foot exam awareness"}
		plan.NextReview = now.AddDate(0, 3, 0)
	case "chf", "heart_failure":
		plan.Goals = []GoalEntry{{Metric: "weight_lb", Target: 185, Unit: "lb", Window: "30d"}}
		plan.Medications = []MedicationEntry{{Name: "Furosemide", Dose: "40 mg", Frequency: "daily", Class: "Loop diuretic"}}
		plan.Monitoring = []MonitoringEntry{
			{Metric: "weight_lb", Cadence: "daily", Source: "rpm", ThresholdMax: 190},
			{Metric: "spo2", Cadence: "daily", Source: "rpm", ThresholdMin: 92},
		}
		plan.Education = []string{"Fluid restriction 2 L/day", "Daily weights at same time"}
		plan.NextReview = now.AddDate(0, 0, 14)
	case "ckd":
		plan.Goals = []GoalEntry{{Metric: "egfr", Target: 60, Unit: "mL/min/1.73m^2", Window: "180d"}}
		plan.Medications = []MedicationEntry{{Name: "Losartan", Dose: "50 mg", Frequency: "daily", Class: "ARB"}}
		plan.Monitoring = []MonitoringEntry{{Metric: "egfr", Cadence: "monthly", Source: "lab"}}
		plan.Education = []string{"Avoid NSAIDs", "Protein-restricted diet"}
		plan.NextReview = now.AddDate(0, 3, 0)
	case "copd":
		plan.Goals = []GoalEntry{{Metric: "spo2", Target: 94, Unit: "%", Window: "30d"}}
		plan.Medications = []MedicationEntry{{Name: "Tiotropium", Dose: "18 mcg", Frequency: "daily", Class: "LAMA"}}
		plan.Monitoring = []MonitoringEntry{{Metric: "spo2", Cadence: "daily", Source: "rpm", ThresholdMin: 90}}
		plan.Education = []string{"Smoking cessation", "Inhaler technique"}
		plan.NextReview = now.AddDate(0, 1, 0)
	case "asthma":
		// Illustrative thresholds only — educational demo, not clinical guideline.
		// ACT score target 20 is the commonly cited "well-controlled" cutoff.
		plan.Goals = []GoalEntry{
			{Metric: "act_score", Target: 20, Unit: "score", Window: "90d"},
			{Metric: "peak_flow_zone", Target: 1, Unit: "personal_best_zone", Window: "90d"},
			{Metric: "exacerbations", Target: 0, Unit: "count", Window: "90d"},
		}
		plan.Medications = []MedicationEntry{
			{Name: "Fluticasone", Dose: "110 mcg", Frequency: "BID", Class: "inhaled_corticosteroid"},
			{Name: "Albuterol", Dose: "90 mcg", Frequency: "PRN", Class: "bronchodilator_short_acting"},
		}
		plan.Monitoring = []MonitoringEntry{
			{Metric: "act_score", Cadence: "quarterly", Source: "self_report"},
			// Peak flow daily for first 30 d then PRN — represented as "daily" here
			// because MonitoringEntry has no phased-cadence field in the demo schema.
			{Metric: "peak_flow", Cadence: "daily", Source: "rpm"},
			{Metric: "exacerbation_count", Cadence: "monthly", Source: "lab"},
		}
		plan.Education = []string{
			"Trigger avoidance",
			"Inhaler technique",
			"Asthma action plan",
			"When to seek emergency care",
		}
		plan.NextReview = now.AddDate(0, 3, 0)
	default:
		plan.Goals = []GoalEntry{{Metric: "encounter_followup", Target: 1, Unit: "visit", Window: "30d"}}
		plan.NextReview = now.AddDate(0, 1, 0)
	}
	return plan
}

// EncodeCarePlan / DecodeCarePlan move plans on the bus as JSON content.
func EncodeCarePlan(p CarePlan) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func DecodeCarePlan(content string) (CarePlan, error) {
	var p CarePlan
	if err := json.Unmarshal([]byte(content), &p); err != nil {
		return CarePlan{}, err
	}
	return p, nil
}

// Summary returns a one-line plan summary for logs/UI.
func (p CarePlan) Summary() string {
	return fmt.Sprintf("plan condition=%s goals=%d meds=%d monitors=%d next_review=%s",
		p.Condition, len(p.Goals), len(p.Medications), len(p.Monitoring),
		p.NextReview.Format("2006-01-02"))
}
