package agents

import (
	"testing"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// TestInferDiagnosisSpecialtyRules exercises every rule branch in the
// rule-based diagnostician with synthetic results that mirror what
// test_planner's simulator emits in production. This guards both the
// rule path itself and the alignment between the simulator's output and
// the diagnostician's pattern matching — drift between them is what the
// SD-Bench gap exposed.
func TestInferDiagnosisSpecialtyRules(t *testing.T) {
	cases := []struct {
		name    string
		state   medical.CaseState
		wantDx  string
		wantMin float64
	}{
		{
			name: "cap → community-acquired pneumonia",
			state: medical.CaseState{
				Condition: "cap",
				TestResults: map[string]string{
					"complete_blood_count":  "WBC 14.2 K, neutrophil predominance",
					"chest_xray":            "Right lower lobe infiltrate consistent with consolidation",
					"basic_metabolic_panel": "Within normal limits",
				},
			},
			wantDx:  "Community-acquired pneumonia",
			wantMin: 0.80,
		},
		{
			name: "acs → NSTEMI (matches manifest gold string)",
			state: medical.CaseState{
				Condition: "acs",
				TestResults: map[string]string{
					"troponin_i":            "Elevated 5.2 ng/mL (reference <0.04)",
					"ecg":                   "ST-segment depression V4-V6 with T-wave inversion",
					"basic_metabolic_panel": "Within normal limits",
					"complete_blood_count":  "WBC 8.1 K, normal differential",
				},
			},
			wantDx:  "Acute coronary syndrome (NSTEMI)",
			wantMin: 0.80,
		},
		{
			name: "sepsis (condition tag) → Urosepsis (matches manifest gold string)",
			state: medical.CaseState{
				Condition: "sepsis",
				TestResults: map[string]string{
					"blood_culture":         "gram-negative rods on preliminary gram stain",
					"lactate":               "Lactate 3.8 mmol/L (elevated)",
					"complete_blood_count":  "WBC 18.4 K, left shift with bands",
					"basic_metabolic_panel": "Creatinine 1.8 mg/dL, bicarbonate 18 mEq/L",
					"urinalysis":            "Pyuria, positive nitrites and leukocyte esterase",
				},
			},
			wantDx:  "Urosepsis",
			wantMin: 0.80,
		},
		{
			name: "dka → Diabetic ketoacidosis",
			state: medical.CaseState{
				Condition: "dka",
				TestResults: map[string]string{
					"basic_metabolic_panel": "Glucose 412 mg/dL, bicarbonate 12 mEq/L, anion gap 22",
					"urine_ketones":         "Ketones 3+ (large)",
					"arterial_blood_gas":    "pH 7.18, pCO2 22, HCO3 11",
					"complete_blood_count":  "WBC 12.5 K, mild leukocytosis",
				},
			},
			wantDx:  "Diabetic ketoacidosis",
			wantMin: 0.80,
		},
		{
			name: "chf via BNP → ADHF",
			state: medical.CaseState{
				Condition: "chf",
				TestResults: map[string]string{
					"bnp":                   "BNP 850 pg/mL (elevated; >400 supports decompensated CHF)",
					"chest_xray":            "Cardiomegaly with bilateral pulmonary edema",
					"ecg":                   "Sinus tachycardia",
					"basic_metabolic_panel": "Creatinine 1.4 mg/dL",
				},
			},
			wantDx:  "Acute decompensated heart failure",
			wantMin: 0.80,
		},
		{
			name: "chf via RPM alerts → ADHF (legacy rule preserved)",
			state: medical.CaseState{
				Condition: "chf",
				RPMAlerts: []string{"elevated_systolic_bp", "weight_gain", "low_spo2"},
			},
			wantDx:  "Acute decompensated heart failure",
			wantMin: 0.80,
		},
		{
			name: "no condition, no signals → Undifferentiated (preserved fallback)",
			state: medical.CaseState{
				TestResults: map[string]string{
					"complete_blood_count":  "WBC 7.0 K",
					"basic_metabolic_panel": "Within normal limits",
				},
			},
			wantDx:  "Undifferentiated respiratory infection — further workup needed",
			wantMin: 0.0,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := inferDiagnosis(tc.state)
			if got.Diagnosis != tc.wantDx {
				t.Errorf("diagnosis = %q, want %q", got.Diagnosis, tc.wantDx)
			}
			if got.Confidence < tc.wantMin {
				t.Errorf("confidence = %.2f, want >= %.2f", got.Confidence, tc.wantMin)
			}
			if got.Rationale == "" {
				t.Errorf("rationale must be non-empty (verifier rejects empty rationales)")
			}
		})
	}
}

// TestExtractFirstNumberHonorsMarker pins down the marker-after-position
// semantics — without this contract, "Glucose 412, bicarbonate 12" would
// return 412 for both markers and the DKA rule (bicarb < 18) would never fire.
func TestExtractFirstNumberHonorsMarker(t *testing.T) {
	bmp := "Glucose 412 mg/dL, bicarbonate 12 mEq/L, anion gap 22"
	if got, ok := extractFirstNumber(bmp, "glucose"); !ok || got != 412 {
		t.Errorf("glucose: got %v ok=%v, want 412 ok=true", got, ok)
	}
	if got, ok := extractFirstNumber(bmp, "bicarbonate"); !ok || got != 12 {
		t.Errorf("bicarbonate: got %v ok=%v, want 12 ok=true", got, ok)
	}
	if got, ok := extractFirstNumber(bmp, "missing_marker"); ok {
		t.Errorf("missing marker should return ok=false, got %v ok=true", got)
	}
}

func TestExtractWBC(t *testing.T) {
	cases := []struct {
		in     string
		want   float64
		wantOk bool
	}{
		{"WBC 14.2 K, neutrophil predominance", 14.2, true},
		{"WBC 18.4 K, left shift", 18.4, true},
		{"No WBC information here", 0, false}, // matches "WBC " but no number → false
		{"", 0, false},
	}
	for _, tc := range cases {
		got, ok := extractWBC(tc.in)
		if ok != tc.wantOk {
			t.Errorf("extractWBC(%q): ok=%v want %v", tc.in, ok, tc.wantOk)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("extractWBC(%q): got %v want %v", tc.in, got, tc.want)
		}
	}
}
