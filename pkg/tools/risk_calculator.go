package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RiskCalculator is a Tool wrapper that dispatches to one of several
// well-known clinical risk scores. The supported calculators are:
//
//   - "wells_pe"    : Wells score for Pulmonary Embolism
//   - "curb65"      : CURB-65 community-acquired pneumonia severity score
//   - "cha2ds2vasc" : CHA2DS2-VASc stroke risk in atrial fibrillation
//   - "has_bled"    : HAS-BLED bleeding risk on anticoagulation
//
// Each sub-calculator implements the standard scoring rules; the banding
// text is condensed for demo use and is not a substitute for vetted clinical
// decision support. Every output carries metadata.source = "demo-synth".
//
// Input shape (JSON):
//
//	{"calculator":"wells_pe","inputs":{...}}
//
// Output shape (JSON):
//
//	{"calculator":"wells_pe","score":4.5,"band":"high",
//	 "components":{...}, "metadata":{"source":"demo-synth"}}
type RiskCalculator struct {
	// calculators is the name → function dispatch table. Populated by
	// DefaultRiskCalculator; the zero-value RiskCalculator has none and
	// returns an error on every Invoke.
	calculators map[string]riskFn
}

// riskFn computes a single calculator from its inputs and returns the
// score, the band text, and the per-component breakdown.
type riskFn func(inputs map[string]any) (score float64, band string, components map[string]any, err error)

// DefaultRiskCalculator returns a RiskCalculator wired to all four
// supported scores. Callers wanting only a subset can build their own with
// a custom calculators map.
func DefaultRiskCalculator() *RiskCalculator {
	return &RiskCalculator{
		calculators: map[string]riskFn{
			"wells_pe":    wellsPE,
			"curb65":      curb65,
			"cha2ds2vasc": cha2ds2vasc,
			"has_bled":    hasBled,
		},
	}
}

// Name implements Tool.
func (*RiskCalculator) Name() string { return "risk_calculator" }

// Description implements Tool.
func (*RiskCalculator) Description() string {
	return "Compute a standard clinical risk score (Wells PE, CURB-65, CHA2DS2-VASc, HAS-BLED) from structured inputs. Returns numeric score, risk band, and per-component breakdown. Demo data tagged source=demo-synth."
}

// Schema implements Tool. The "inputs" object is left loosely typed — each
// sub-calculator validates the keys it expects — because OpenAI strict mode
// rejects oneOf at the same level as "additionalProperties":false. The
// description on inputs names every supported key.
func (*RiskCalculator) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "calculator": {
      "type": "string",
      "enum": ["wells_pe", "curb65", "cha2ds2vasc", "has_bled"],
      "description": "Which score to compute."
    },
    "inputs": {
      "type": "object",
      "description": "Per-calculator inputs. wells_pe: clinical_signs_dvt, alt_dx_less_likely, hr_over_100, immobilization_or_surgery, prev_dvt_pe, hemoptysis, malignancy (all boolean). curb65: confusion (bool), urea_mmol_l (number), respiratory_rate (number), sbp (number), dbp (number), age (number). cha2ds2vasc: chf (bool), hypertension (bool), age (number), diabetes (bool), stroke_tia (bool), vascular_disease (bool), female (bool). has_bled: hypertension (bool), abnormal_renal (bool), abnormal_liver (bool), stroke (bool), bleeding_history (bool), labile_inr (bool), elderly (bool), drugs_or_alcohol (number 0-2)."
    }
  },
  "required": ["calculator", "inputs"],
  "additionalProperties": false
}`)
}

// riskInput mirrors the JSON shape Invoke accepts.
type riskInput struct {
	Calculator string         `json:"calculator"`
	Inputs     map[string]any `json:"inputs"`
}

// riskOutput is the JSON Invoke returns.
type riskOutput struct {
	Calculator string         `json:"calculator"`
	Score      float64        `json:"score"`
	Band       string         `json:"band"`
	Components map[string]any `json:"components"`
	Metadata   map[string]any `json:"metadata"`
}

// Invoke implements Tool.
func (r *RiskCalculator) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("risk_calculator: empty arguments")
	}
	var in riskInput
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, fmt.Errorf("risk_calculator: decode arguments: %w", err)
	}
	name := strings.ToLower(strings.TrimSpace(in.Calculator))
	fn, ok := r.calculators[name]
	if !ok {
		return nil, fmt.Errorf("risk_calculator: unknown calculator %q", in.Calculator)
	}
	score, band, components, err := fn(in.Inputs)
	if err != nil {
		return nil, fmt.Errorf("risk_calculator/%s: %w", name, err)
	}
	out := riskOutput{
		Calculator: name,
		Score:      score,
		Band:       band,
		Components: components,
		Metadata:   map[string]any{"source": "demo-synth"},
	}
	return json.Marshal(out)
}

// boolField looks up key in inputs and returns its bool value, treating
// missing or non-bool entries as false. We deliberately do not error on a
// missing key — clinical inputs are often sparse, and the absence of a flag
// is meaningful (e.g. no DVT signs).
func boolField(inputs map[string]any, key string) bool {
	v, ok := inputs[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// numField looks up key in inputs and returns its float64 value, treating
// missing or non-numeric entries as 0. JSON numbers decode to float64, so
// integer inputs round-trip cleanly.
func numField(inputs map[string]any, key string) float64 {
	v, ok := inputs[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// wellsPE implements the Wells score for pulmonary embolism. The standard
// scoring is: clinical signs of DVT (+3.0), alternative diagnosis less likely
// than PE (+3.0), HR > 100 (+1.5), immobilization or surgery in the past 4w
// (+1.5), previous DVT/PE (+1.5), hemoptysis (+1.0), malignancy (+1.0).
// Bands: <2 low, 2-6 moderate, >6 high.
func wellsPE(inputs map[string]any) (float64, string, map[string]any, error) {
	score := 0.0
	components := map[string]any{}

	add := func(key string, weight float64) {
		if boolField(inputs, key) {
			score += weight
			components[key] = weight
		} else {
			components[key] = 0.0
		}
	}
	add("clinical_signs_dvt", 3.0)
	add("alt_dx_less_likely", 3.0)
	add("hr_over_100", 1.5)
	add("immobilization_or_surgery", 1.5)
	add("prev_dvt_pe", 1.5)
	add("hemoptysis", 1.0)
	add("malignancy", 1.0)

	band := "low"
	switch {
	case score > 6:
		band = "high"
	case score >= 2:
		band = "moderate"
	}
	return score, band, components, nil
}

// curb65 implements CURB-65: 1 point each for Confusion, Urea > 7 mmol/L,
// Respiratory rate >= 30, low blood pressure (SBP < 90 or DBP <= 60), and
// age >= 65. Disposition guidance: 0-1 outpatient, 2 inpatient, >=3
// inpatient with ICU consideration.
func curb65(inputs map[string]any) (float64, string, map[string]any, error) {
	score := 0.0
	components := map[string]any{}

	if boolField(inputs, "confusion") {
		score++
		components["confusion"] = 1
	} else {
		components["confusion"] = 0
	}
	if numField(inputs, "urea_mmol_l") > 7 {
		score++
		components["urea_over_7"] = 1
	} else {
		components["urea_over_7"] = 0
	}
	if numField(inputs, "respiratory_rate") >= 30 {
		score++
		components["rr_ge_30"] = 1
	} else {
		components["rr_ge_30"] = 0
	}
	sbp := numField(inputs, "sbp")
	dbp := numField(inputs, "dbp")
	if (sbp > 0 && sbp < 90) || (dbp > 0 && dbp <= 60) {
		score++
		components["low_bp"] = 1
	} else {
		components["low_bp"] = 0
	}
	if numField(inputs, "age") >= 65 {
		score++
		components["age_ge_65"] = 1
	} else {
		components["age_ge_65"] = 0
	}

	band := "outpatient"
	switch {
	case score >= 3:
		band = "icu_consideration"
	case score == 2:
		band = "inpatient"
	}
	return score, band, components, nil
}

// cha2ds2vasc implements CHA2DS2-VASc stroke risk in non-valvular AF:
// CHF (+1), Hypertension (+1), Age 65-74 (+1) or >=75 (+2), Diabetes (+1),
// prior Stroke/TIA (+2), Vascular disease (+1), Female sex (+1). Bands:
// 0 low, 1 moderate, >=2 high.
func cha2ds2vasc(inputs map[string]any) (float64, string, map[string]any, error) {
	score := 0.0
	components := map[string]any{}

	addBool := func(key string, weight float64) {
		if boolField(inputs, key) {
			score += weight
			components[key] = weight
		} else {
			components[key] = 0.0
		}
	}
	addBool("chf", 1)
	addBool("hypertension", 1)
	age := numField(inputs, "age")
	switch {
	case age >= 75:
		score += 2
		components["age"] = 2
	case age >= 65:
		score += 1
		components["age"] = 1
	default:
		components["age"] = 0
	}
	addBool("diabetes", 1)
	addBool("stroke_tia", 2)
	addBool("vascular_disease", 1)
	addBool("female", 1)

	band := "low"
	switch {
	case score >= 2:
		band = "high"
	case score == 1:
		band = "moderate"
	}
	return score, band, components, nil
}

// hasBled implements HAS-BLED bleeding risk on anticoagulation: Hypertension
// (+1), Abnormal renal (+1), Abnormal liver (+1), Stroke (+1), Bleeding
// history (+1), Labile INR (+1), Elderly (>65, +1), Drugs/alcohol (0-2).
// Bands: 0-2 low, 3+ high (consider risk-benefit).
func hasBled(inputs map[string]any) (float64, string, map[string]any, error) {
	score := 0.0
	components := map[string]any{}

	addBool := func(key string) {
		if boolField(inputs, key) {
			score++
			components[key] = 1
		} else {
			components[key] = 0
		}
	}
	addBool("hypertension")
	addBool("abnormal_renal")
	addBool("abnormal_liver")
	addBool("stroke")
	addBool("bleeding_history")
	addBool("labile_inr")
	addBool("elderly")

	drugs := numField(inputs, "drugs_or_alcohol")
	if drugs < 0 {
		drugs = 0
	}
	if drugs > 2 {
		drugs = 2
	}
	score += drugs
	components["drugs_or_alcohol"] = drugs

	band := "low"
	if score >= 3 {
		band = "high"
	}
	return score, band, components, nil
}
