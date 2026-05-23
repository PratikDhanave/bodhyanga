package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func invokeRisk(t *testing.T, args string) riskOutput {
	t.Helper()
	raw, err := DefaultRiskCalculator().Invoke(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var out riskOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, string(raw))
	}
	return out
}

func TestRisk_WellsPE_HighBand(t *testing.T) {
	// All seven heavily-weighted criteria true → score = 12.5, band high.
	out := invokeRisk(t, `{
        "calculator":"wells_pe",
        "inputs":{
            "clinical_signs_dvt": true,
            "alt_dx_less_likely": true,
            "hr_over_100": true,
            "immobilization_or_surgery": true,
            "prev_dvt_pe": true,
            "hemoptysis": true,
            "malignancy": true
        }
    }`)
	if out.Calculator != "wells_pe" {
		t.Fatalf("calculator=%q, want wells_pe", out.Calculator)
	}
	if out.Score != 12.5 {
		t.Fatalf("score=%v, want 12.5", out.Score)
	}
	if out.Band != "high" {
		t.Fatalf("band=%q, want high", out.Band)
	}
}

func TestRisk_WellsPE_LowBand(t *testing.T) {
	// Only hemoptysis (+1) → score below 2 → low band.
	out := invokeRisk(t, `{
        "calculator":"wells_pe",
        "inputs":{"hemoptysis": true}
    }`)
	if out.Score != 1.0 {
		t.Fatalf("score=%v, want 1.0", out.Score)
	}
	if out.Band != "low" {
		t.Fatalf("band=%q, want low", out.Band)
	}
}

func TestRisk_CURB65_ICUBand(t *testing.T) {
	// All five criteria positive → score 5 → ICU consideration.
	out := invokeRisk(t, `{
        "calculator":"curb65",
        "inputs":{
            "confusion": true,
            "urea_mmol_l": 8.5,
            "respiratory_rate": 32,
            "sbp": 85,
            "dbp": 55,
            "age": 78
        }
    }`)
	if out.Score != 5 {
		t.Fatalf("score=%v, want 5", out.Score)
	}
	if out.Band != "icu_consideration" {
		t.Fatalf("band=%q, want icu_consideration", out.Band)
	}
}

func TestRisk_CURB65_OutpatientBand(t *testing.T) {
	// Healthy young patient — no positive criteria → outpatient.
	out := invokeRisk(t, `{
        "calculator":"curb65",
        "inputs":{
            "confusion": false,
            "urea_mmol_l": 4.5,
            "respiratory_rate": 18,
            "sbp": 120,
            "dbp": 80,
            "age": 35
        }
    }`)
	if out.Score != 0 {
		t.Fatalf("score=%v, want 0", out.Score)
	}
	if out.Band != "outpatient" {
		t.Fatalf("band=%q, want outpatient", out.Band)
	}
}

func TestRisk_CHA2DS2VASc_HighBand(t *testing.T) {
	// 80-year-old female with HTN and CHF: 2 (age) + 1 (HTN) + 1 (CHF) + 1
	// (female) = 5 → high band.
	out := invokeRisk(t, `{
        "calculator":"cha2ds2vasc",
        "inputs":{
            "chf": true,
            "hypertension": true,
            "age": 80,
            "female": true
        }
    }`)
	if out.Score != 5 {
		t.Fatalf("score=%v, want 5", out.Score)
	}
	if out.Band != "high" {
		t.Fatalf("band=%q, want high", out.Band)
	}
}

func TestRisk_CHA2DS2VASc_ModerateOnAge6574(t *testing.T) {
	// 68-year-old male with no other risk → score 1 → moderate band.
	out := invokeRisk(t, `{
        "calculator":"cha2ds2vasc",
        "inputs":{"age": 68}
    }`)
	if out.Score != 1 {
		t.Fatalf("score=%v, want 1", out.Score)
	}
	if out.Band != "moderate" {
		t.Fatalf("band=%q, want moderate", out.Band)
	}
}

func TestRisk_HASBLED_HighBand(t *testing.T) {
	// Elderly hypertensive with renal disease + bleeding history + 1 risky
	// drug → score 5 → high band.
	out := invokeRisk(t, `{
        "calculator":"has_bled",
        "inputs":{
            "hypertension": true,
            "abnormal_renal": true,
            "bleeding_history": true,
            "elderly": true,
            "drugs_or_alcohol": 1
        }
    }`)
	if out.Score != 5 {
		t.Fatalf("score=%v, want 5", out.Score)
	}
	if out.Band != "high" {
		t.Fatalf("band=%q, want high", out.Band)
	}
}

func TestRisk_HASBLED_DrugsClampedToTwo(t *testing.T) {
	// Drugs/alcohol field is bounded to [0, 2] — passing 5 must clamp to 2.
	out := invokeRisk(t, `{
        "calculator":"has_bled",
        "inputs":{"drugs_or_alcohol": 5}
    }`)
	if out.Score != 2 {
		t.Fatalf("score=%v, want 2 (clamped)", out.Score)
	}
	if got, ok := out.Components["drugs_or_alcohol"].(float64); !ok || got != 2 {
		t.Fatalf("components.drugs_or_alcohol=%v, want 2", out.Components["drugs_or_alcohol"])
	}
}

func TestRisk_UnknownCalculatorErrors(t *testing.T) {
	_, err := DefaultRiskCalculator().Invoke(context.Background(), json.RawMessage(`{"calculator":"nope","inputs":{}}`))
	if err == nil {
		t.Fatalf("expected error on unknown calculator")
	}
	if !strings.Contains(err.Error(), "unknown calculator") {
		t.Fatalf("error=%q, want 'unknown calculator'", err.Error())
	}
}

func TestRisk_MetadataSourceAlwaysDemoSynth(t *testing.T) {
	out := invokeRisk(t, `{"calculator":"wells_pe","inputs":{}}`)
	if out.Metadata["source"] != "demo-synth" {
		t.Fatalf("metadata.source=%v, want demo-synth", out.Metadata["source"])
	}
}
