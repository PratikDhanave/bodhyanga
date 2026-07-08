package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// ---------------------------------------------------------------------------
// WeightBasedDosingTool
// ---------------------------------------------------------------------------

// WeightBasedDosing calculates a drug dose by patient weight and optionally
// caps it at a maximum dose. It is illustrative only and must not be used
// for actual clinical decisions without pharmacist / physician review.
//
// Input shape (JSON):
//
//	{
//	  "drug": "amoxicillin",
//	  "dose_per_kg": 25.0,
//	  "weight_kg": 20.0,
//	  "max_dose_mg": 500.0,  // optional
//	  "route": "oral"        // optional
//	}
//
// Output shape (JSON):
//
//	{
//	  "calculated_dose_mg": 500.0,
//	  "capped": false,
//	  "instruction": "Give amoxicillin 500.0 mg orally"
//	}
type WeightBasedDosing struct{}

func (WeightBasedDosing) Name() string { return "weight_based_dose" }

func (WeightBasedDosing) Description() string {
	return "Calculate a drug dose by patient weight (mg/kg). Optionally caps the result at a maximum dose. Returns the calculated dose in mg and a plain-English instruction. Demo data only — not for clinical use without review."
}

func (WeightBasedDosing) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "drug": {
      "type": "string",
      "description": "Generic drug name (e.g. \"amoxicillin\")"
    },
    "dose_per_kg": {
      "type": "number",
      "description": "Dose in mg per kg of body weight"
    },
    "weight_kg": {
      "type": "number",
      "description": "Patient weight in kilograms"
    },
    "max_dose_mg": {
      "type": "number",
      "description": "Optional maximum dose in mg; calculated dose will be capped to this value"
    },
    "route": {
      "type": "string",
      "description": "Administration route (e.g. \"oral\", \"IV\", \"IM\"); defaults to \"oral\" when omitted"
    }
  },
  "required": ["drug", "dose_per_kg", "weight_kg"],
  "additionalProperties": false
}`)
}

type weightBasedDosingInput struct {
	Drug      string   `json:"drug"`
	DosePerKg float64  `json:"dose_per_kg"`
	WeightKg  float64  `json:"weight_kg"`
	MaxDoseMg *float64 `json:"max_dose_mg,omitempty"`
	Route     string   `json:"route,omitempty"`
}

type weightBasedDosingOutput struct {
	CalculatedDoseMg float64 `json:"calculated_dose_mg"`
	Capped           bool    `json:"capped"`
	Instruction      string  `json:"instruction"`
}

func (WeightBasedDosing) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("weight_based_dose: empty arguments")
	}
	var in weightBasedDosingInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("weight_based_dose: decode arguments: %w", err)
	}
	drug := strings.TrimSpace(in.Drug)
	if drug == "" {
		return nil, fmt.Errorf("weight_based_dose: drug name is required")
	}
	if in.DosePerKg <= 0 {
		return nil, fmt.Errorf("weight_based_dose: dose_per_kg must be positive")
	}
	if in.WeightKg <= 0 {
		return nil, fmt.Errorf("weight_based_dose: weight_kg must be positive")
	}

	route := in.Route
	if route == "" {
		route = "oral"
	}

	calculated := in.DosePerKg * in.WeightKg
	capped := false
	if in.MaxDoseMg != nil && calculated > *in.MaxDoseMg {
		calculated = *in.MaxDoseMg
		capped = true
	}

	// Round to two decimal places for readability.
	calculated = math.Round(calculated*100) / 100

	instruction := fmt.Sprintf("Give %s %.2f mg %s", drug, calculated, route)
	if capped {
		instruction += " (dose capped at maximum)"
	}

	out := weightBasedDosingOutput{
		CalculatedDoseMg: calculated,
		Capped:           capped,
		Instruction:      instruction,
	}
	return json.Marshal(out)
}

// ---------------------------------------------------------------------------
// RenalDosingTool
// ---------------------------------------------------------------------------

// RenalDosing adjusts a drug dose for impaired kidney function using the
// Cockcroft-Gault eGFR estimate. The adjustment table covers five common
// drugs as an illustration.
//
// Input shape (JSON):
//
//	{
//	  "drug": "metformin",
//	  "standard_dose_mg": 500.0,
//	  "creatinine_mg_dl": 1.8,
//	  "age": 65,
//	  "weight_kg": 70.0,
//	  "sex": "male"
//	}
//
// Output shape (JSON):
//
//	{
//	  "egfr_ml_min": 42.3,
//	  "ckd_stage": "G3b",
//	  "adjusted_dose_mg": 0,
//	  "frequency": "hold",
//	  "notes": "metformin: hold if eGFR < 30 ..."
//	}
type RenalDosing struct{}

func (RenalDosing) Name() string { return "renal_dose_adjust" }

func (RenalDosing) Description() string {
	return "Adjust a drug dose for impaired kidney function using the Cockcroft-Gault eGFR formula. Covers metformin, gabapentin, lisinopril, digoxin, and vancomycin. Demo data only — not for clinical use without pharmacist review."
}

func (RenalDosing) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "drug": {
      "type": "string",
      "description": "Generic drug name (metformin, gabapentin, lisinopril, digoxin, vancomycin)"
    },
    "standard_dose_mg": {
      "type": "number",
      "description": "Standard (full-function) dose in mg"
    },
    "creatinine_mg_dl": {
      "type": "number",
      "description": "Serum creatinine in mg/dL"
    },
    "age": {
      "type": "integer",
      "description": "Patient age in years"
    },
    "weight_kg": {
      "type": "number",
      "description": "Patient weight in kilograms"
    },
    "sex": {
      "type": "string",
      "description": "Patient sex: \"male\" or \"female\" (used for Cockcroft-Gault sex correction)"
    }
  },
  "required": ["drug", "standard_dose_mg", "creatinine_mg_dl", "age", "weight_kg", "sex"],
  "additionalProperties": false
}`)
}

type renalDosingInput struct {
	Drug           string  `json:"drug"`
	StandardDoseMg float64 `json:"standard_dose_mg"`
	CreatinineMgDl float64 `json:"creatinine_mg_dl"`
	Age            int     `json:"age"`
	WeightKg       float64 `json:"weight_kg"`
	Sex            string  `json:"sex"`
}

type renalDosingOutput struct {
	EgfrMlMin      float64 `json:"egfr_ml_min"`
	CkdStage       string  `json:"ckd_stage"`
	AdjustedDoseMg float64 `json:"adjusted_dose_mg"`
	Frequency      string  `json:"frequency"`
	Notes          string  `json:"notes"`
}

// cockcroftGault returns eGFR in mL/min using the Cockcroft-Gault equation.
//
//	eGFR = ((140 - age) * weight_kg * sex_factor) / (72 * creatinine_mg_dl)
//
// sex_factor is 0.85 for female, 1.0 for male.
func cockcroftGault(age int, weightKg, creatinineMgDl float64, sex string) float64 {
	sexFactor := 1.0
	if strings.EqualFold(strings.TrimSpace(sex), "female") {
		sexFactor = 0.85
	}
	egfr := (float64(140-age) * weightKg * sexFactor) / (72.0 * creatinineMgDl)
	if egfr < 0 {
		egfr = 0
	}
	return math.Round(egfr*10) / 10
}

// ckdStage classifies eGFR into a KDIGO CKD stage label.
func ckdStage(egfr float64) string {
	switch {
	case egfr >= 90:
		return "G1"
	case egfr >= 60:
		return "G2"
	case egfr >= 45:
		return "G3a"
	case egfr >= 30:
		return "G3b"
	case egfr >= 15:
		return "G4"
	default:
		return "G5"
	}
}

// renalAdjustmentTable maps lower-cased drug names to an adjustment function
// that takes (standardDose float64, egfr float64) and returns (adjustedDose,
// frequency, notes).
var renalAdjustmentTable = map[string]func(stdDose, egfr float64) (float64, string, string){
	"metformin": func(stdDose, egfr float64) (float64, string, string) {
		if egfr < 30 {
			return 0, "hold", "metformin: contraindicated when eGFR < 30 mL/min due to lactic acidosis risk; discontinue and reassess when renal function improves"
		}
		return stdDose, "as prescribed", "metformin: eGFR ≥ 30 — use with caution; monitor renal function every 3-6 months"
	},
	"gabapentin": func(stdDose, egfr float64) (float64, string, string) {
		switch {
		case egfr < 30:
			adj := math.Round(stdDose*0.25*100) / 100
			return adj, "per protocol", "gabapentin: reduce dose by 75% for eGFR < 30 mL/min; supplement dose after each dialysis session if applicable"
		case egfr < 60:
			adj := math.Round(stdDose*0.5*100) / 100
			return adj, "per protocol", "gabapentin: reduce dose by 50% for eGFR 30-60 mL/min; monitor for CNS side effects"
		default:
			return stdDose, "as prescribed", "gabapentin: no dose adjustment required for eGFR ≥ 60 mL/min"
		}
	},
	"lisinopril": func(stdDose, egfr float64) (float64, string, string) {
		if egfr < 30 {
			return 2.5, "once daily", "lisinopril: initiate at 2.5 mg once daily for eGFR < 30 mL/min; titrate carefully; monitor potassium and creatinine closely"
		}
		return stdDose, "as prescribed", "lisinopril: standard dosing for eGFR ≥ 30 mL/min; monitor renal function and electrolytes"
	},
	"digoxin": func(stdDose, egfr float64) (float64, string, string) {
		if egfr < 50 {
			adj := math.Round(stdDose*0.625*100) / 100 // midpoint of 25-50% reduction
			return adj, "as prescribed", fmt.Sprintf("digoxin: reduce dose 25-50%% for eGFR < 50 mL/min (adjusted to %.2f mg); monitor serum digoxin levels and signs of toxicity", adj)
		}
		return stdDose, "as prescribed", "digoxin: standard dosing for eGFR ≥ 50 mL/min; routine level monitoring recommended"
	},
	"vancomycin": func(stdDose, egfr float64) (float64, string, string) {
		switch {
		case egfr < 10:
			return stdDose, "q48h", "vancomycin: extend interval to every 48 hours for eGFR < 10 mL/min; monitor trough levels before each dose"
		case egfr < 50:
			return stdDose, "q24h", "vancomycin: extend interval to every 24 hours for eGFR 10-50 mL/min; monitor trough levels"
		case egfr < 90:
			return stdDose, "q12h", "vancomycin: extend interval to every 12 hours for eGFR 50-90 mL/min; monitor trough levels"
		default:
			return stdDose, "q8-12h", "vancomycin: standard interval (q8-12h) for eGFR ≥ 90 mL/min; monitor trough levels"
		}
	},
}

func (RenalDosing) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("renal_dose_adjust: empty arguments")
	}
	var in renalDosingInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("renal_dose_adjust: decode arguments: %w", err)
	}
	drug := strings.ToLower(strings.TrimSpace(in.Drug))
	if drug == "" {
		return nil, fmt.Errorf("renal_dose_adjust: drug name is required")
	}
	if in.StandardDoseMg <= 0 {
		return nil, fmt.Errorf("renal_dose_adjust: standard_dose_mg must be positive")
	}
	if in.CreatinineMgDl <= 0 {
		return nil, fmt.Errorf("renal_dose_adjust: creatinine_mg_dl must be positive")
	}
	if in.Age <= 0 || in.Age > 130 {
		return nil, fmt.Errorf("renal_dose_adjust: age must be between 1 and 130")
	}
	if in.WeightKg <= 0 {
		return nil, fmt.Errorf("renal_dose_adjust: weight_kg must be positive")
	}

	egfr := cockcroftGault(in.Age, in.WeightKg, in.CreatinineMgDl, in.Sex)
	stage := ckdStage(egfr)

	adjFn, ok := renalAdjustmentTable[drug]
	if !ok {
		// Unknown drug: return eGFR info with a note that no table entry exists.
		out := renalDosingOutput{
			EgfrMlMin:      egfr,
			CkdStage:       stage,
			AdjustedDoseMg: in.StandardDoseMg,
			Frequency:      "as prescribed",
			Notes:          fmt.Sprintf("%s: no renal adjustment table entry available; consult clinical pharmacist for eGFR %.1f mL/min (%s)", drug, egfr, stage),
		}
		return json.Marshal(out)
	}

	adjustedDose, frequency, notes := adjFn(in.StandardDoseMg, egfr)
	out := renalDosingOutput{
		EgfrMlMin:      egfr,
		CkdStage:       stage,
		AdjustedDoseMg: adjustedDose,
		Frequency:      frequency,
		Notes:          notes,
	}
	return json.Marshal(out)
}

// ---------------------------------------------------------------------------
// PediatricDosingTool
// ---------------------------------------------------------------------------

// PediatricDosing returns weight-based pediatric doses for a set of common
// drugs. All doses are illustrative and must be verified by a pharmacist or
// paediatric physician before use.
//
// Input shape (JSON):
//
//	{"drug": "ibuprofen", "weight_kg": 20.0, "age_months": 48}
//
// Output shape (JSON):
//
//	{
//	  "dose_mg": 100.0,
//	  "frequency": "q6-8h",
//	  "max_daily_mg": 800.0,
//	  "notes": "ibuprofen: 5 mg/kg/dose ..."
//	}
type PediatricDosing struct{}

func (PediatricDosing) Name() string { return "pediatric_dose" }

func (PediatricDosing) Description() string {
	return "Return weight-based pediatric doses for common drugs (amoxicillin, ibuprofen, acetaminophen, azithromycin). Validates against maximum daily doses. Demo data only — not for clinical use without paediatric pharmacist review."
}

func (PediatricDosing) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "drug": {
      "type": "string",
      "description": "Generic drug name: amoxicillin, ibuprofen, acetaminophen, azithromycin"
    },
    "weight_kg": {
      "type": "number",
      "description": "Patient weight in kilograms"
    },
    "age_months": {
      "type": "integer",
      "description": "Patient age in months"
    }
  },
  "required": ["drug", "weight_kg", "age_months"],
  "additionalProperties": false
}`)
}

type pediatricDosingInput struct {
	Drug      string  `json:"drug"`
	WeightKg  float64 `json:"weight_kg"`
	AgeMonths int     `json:"age_months"`
}

type pediatricDosingOutput struct {
	DoseMg     float64 `json:"dose_mg"`
	Frequency  string  `json:"frequency"`
	MaxDailyMg float64 `json:"max_daily_mg"`
	Notes      string  `json:"notes"`
}

// pediatricDrugEntry holds the dosing parameters for one pediatric drug.
type pediatricDrugEntry struct {
	// dosePerKgPerDose is mg/kg/dose (used for a single administration).
	dosePerKgPerDose float64
	// maxDailyPerKg is the maximum total daily dose in mg/kg.
	maxDailyPerKg float64
	frequency     string
	notes         func(weightKg float64, ageMonths int, doseMg, maxDaily float64) string
}

// pediatricDrugTable maps lower-cased drug names to their dosing parameters.
// Azithromycin is handled specially because day-1 and days-2–5 differ; it is
// listed in the table with a sentinel and resolved below.
var pediatricDrugTable = map[string]pediatricDrugEntry{
	"amoxicillin": {
		dosePerKgPerDose: 15, // 45 mg/kg/day ÷ 3 doses
		maxDailyPerKg:    90, // AOM high-dose
		frequency:        "q8h",
		notes: func(weightKg float64, _ int, doseMg, maxDaily float64) string {
			return fmt.Sprintf(
				"amoxicillin: standard 45 mg/kg/day divided q8h; single dose %.0f mg (%.0f mg/day). "+
					"High-dose AOM protocol: up to 90 mg/kg/day — max daily dose capped at %.0f mg. "+
					"Demo data only.",
				doseMg, doseMg*3, maxDaily,
			)
		},
	},
	"ibuprofen": {
		dosePerKgPerDose: 7.5, // midpoint of 5-10 mg/kg/dose
		maxDailyPerKg:    40,
		frequency:        "q6-8h",
		notes: func(weightKg float64, _ int, doseMg, maxDaily float64) string {
			return fmt.Sprintf(
				"ibuprofen: 5-10 mg/kg/dose q6-8h; single dose %.0f mg (7.5 mg/kg mid-range). "+
					"Maximum daily dose %.0f mg (40 mg/kg/day). Do not use in infants < 6 months. "+
					"Demo data only.",
				doseMg, maxDaily,
			)
		},
	},
	"acetaminophen": {
		dosePerKgPerDose: 12.5, // midpoint of 10-15 mg/kg/dose
		maxDailyPerKg:    75,
		frequency:        "q4-6h",
		notes: func(weightKg float64, _ int, doseMg, maxDaily float64) string {
			return fmt.Sprintf(
				"acetaminophen: 10-15 mg/kg/dose q4-6h; single dose %.0f mg (12.5 mg/kg mid-range). "+
					"Maximum daily dose %.0f mg (75 mg/kg/day, max 5 doses/24h). Demo data only.",
				doseMg, maxDaily,
			)
		},
	},
}

func (PediatricDosing) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("pediatric_dose: empty arguments")
	}
	var in pediatricDosingInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("pediatric_dose: decode arguments: %w", err)
	}
	drug := strings.ToLower(strings.TrimSpace(in.Drug))
	if drug == "" {
		return nil, fmt.Errorf("pediatric_dose: drug name is required")
	}
	if in.WeightKg <= 0 {
		return nil, fmt.Errorf("pediatric_dose: weight_kg must be positive")
	}
	if in.AgeMonths < 0 {
		return nil, fmt.Errorf("pediatric_dose: age_months must be non-negative")
	}

	// Azithromycin uses a 5-day Z-pack schedule: 10 mg/kg day 1, 5 mg/kg days 2-5.
	if drug == "azithromycin" {
		day1Dose := math.Round(10.0*in.WeightKg*100) / 100
		maintenanceDose := math.Round(5.0*in.WeightKg*100) / 100
		// Represent max daily as day-1 dose (highest single-day exposure).
		maxDaily := day1Dose
		notes := fmt.Sprintf(
			"azithromycin: day 1 dose %.0f mg (10 mg/kg), then %.0f mg (5 mg/kg) once daily for days 2-5. "+
				"Total course 5 days. Not recommended for infants < 6 months. Demo data only.",
			day1Dose, maintenanceDose,
		)
		out := pediatricDosingOutput{
			DoseMg:     day1Dose,
			Frequency:  "once daily (5-day course)",
			MaxDailyMg: maxDaily,
			Notes:      notes,
		}
		return json.Marshal(out)
	}

	entry, ok := pediatricDrugTable[drug]
	if !ok {
		return nil, fmt.Errorf("pediatric_dose: drug %q not in table; supported drugs: amoxicillin, ibuprofen, acetaminophen, azithromycin", drug)
	}

	doseMg := math.Round(entry.dosePerKgPerDose*in.WeightKg*100) / 100
	maxDailyMg := math.Round(entry.maxDailyPerKg*in.WeightKg*100) / 100

	out := pediatricDosingOutput{
		DoseMg:     doseMg,
		Frequency:  entry.frequency,
		MaxDailyMg: maxDailyMg,
		Notes:      entry.notes(in.WeightKg, in.AgeMonths, doseMg, maxDailyMg),
	}
	return json.Marshal(out)
}
