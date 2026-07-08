package tools

import (
	"context"
	"encoding/json"
	"math"
	"testing"
)

// roundAt2 rounds f to two decimal places for assertion comparisons where
// floating-point arithmetic can introduce tiny deltas.
func roundAt2(f float64) float64 { return math.Round(f*100) / 100 }

// -------------------------------------------------------------------------
// WeightBasedDosing tests
// -------------------------------------------------------------------------

func TestDosingWeightBased_SimpleCalculation(t *testing.T) {
	tool := WeightBasedDosing{}
	args := json.RawMessage(`{"drug":"amoxicillin","dose_per_kg":25,"weight_kg":20}`)
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out weightBasedDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out.CalculatedDoseMg != 500 {
		t.Errorf("expected 500 mg, got %v", out.CalculatedDoseMg)
	}
	if out.Capped {
		t.Error("expected capped=false")
	}
}

func TestDosingWeightBased_CappedAtMax(t *testing.T) {
	tool := WeightBasedDosing{}
	maxDose := 400.0
	args, _ := json.Marshal(map[string]interface{}{
		"drug":        "amoxicillin",
		"dose_per_kg": 25,
		"weight_kg":   20, // would be 500 mg without cap
		"max_dose_mg": maxDose,
		"route":       "oral",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out weightBasedDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out.CalculatedDoseMg != maxDose {
		t.Errorf("expected dose capped at %.0f, got %v", maxDose, out.CalculatedDoseMg)
	}
	if !out.Capped {
		t.Error("expected capped=true")
	}
}

func TestDosingWeightBased_BelowMax_NotCapped(t *testing.T) {
	tool := WeightBasedDosing{}
	maxDose := 1000.0
	args, _ := json.Marshal(map[string]interface{}{
		"drug":        "penicillin",
		"dose_per_kg": 10,
		"weight_kg":   15,
		"max_dose_mg": maxDose,
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out weightBasedDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	// 10 * 15 = 150, below cap of 1000
	if out.CalculatedDoseMg != 150 {
		t.Errorf("expected 150 mg, got %v", out.CalculatedDoseMg)
	}
	if out.Capped {
		t.Error("expected capped=false")
	}
}

func TestDosingWeightBased_DefaultRoute(t *testing.T) {
	tool := WeightBasedDosing{}
	args := json.RawMessage(`{"drug":"metronidazole","dose_per_kg":7.5,"weight_kg":10}`)
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out weightBasedDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	// 7.5 * 10 = 75
	if out.CalculatedDoseMg != 75 {
		t.Errorf("expected 75 mg, got %v", out.CalculatedDoseMg)
	}
	// Instruction should mention "oral" as default route
	if len(out.Instruction) == 0 {
		t.Error("instruction is empty")
	}
}

func TestDosingWeightBased_EmptyArgs(t *testing.T) {
	tool := WeightBasedDosing{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args, got nil")
	}
}

func TestDosingWeightBased_MissingDrug(t *testing.T) {
	tool := WeightBasedDosing{}
	args := json.RawMessage(`{"drug":"","dose_per_kg":10,"weight_kg":20}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty drug name, got nil")
	}
}

func TestDosingWeightBased_NegativeWeight(t *testing.T) {
	tool := WeightBasedDosing{}
	args := json.RawMessage(`{"drug":"ibuprofen","dose_per_kg":10,"weight_kg":-5}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for negative weight, got nil")
	}
}

// -------------------------------------------------------------------------
// RenalDosing tests — CockcroftGault
// -------------------------------------------------------------------------

// round1 rounds f to one decimal place, matching cockcroftGault's output precision.
func round1(f float64) float64 { return math.Round(f*10) / 10 }

func TestDosingCockcroftGault_Male(t *testing.T) {
	// ((140-65)*70*1.0)/(72*1.5) = 5250/108 ≈ 48.6
	egfr := cockcroftGault(65, 70, 1.5, "male")
	want := round1((float64(140-65) * 70.0 * 1.0) / (72.0 * 1.5))
	if egfr != want {
		t.Errorf("eGFR male: got %.1f, want %.1f", egfr, want)
	}
}

func TestDosingCockcroftGault_Female(t *testing.T) {
	// ((140-65)*70*0.85)/(72*1.5) ≈ 41.3
	egfr := cockcroftGault(65, 70, 1.5, "female")
	want := round1((float64(140-65) * 70.0 * 0.85) / (72.0 * 1.5))
	if egfr != want {
		t.Errorf("eGFR female: got %.1f, want %.1f", egfr, want)
	}
}

func TestDosingCKDStage(t *testing.T) {
	cases := []struct {
		egfr  float64
		stage string
	}{
		{95, "G1"},
		{75, "G2"},
		{50, "G3a"},
		{35, "G3b"},
		{20, "G4"},
		{5, "G5"},
	}
	for _, tc := range cases {
		got := ckdStage(tc.egfr)
		if got != tc.stage {
			t.Errorf("ckdStage(%.0f) = %q, want %q", tc.egfr, got, tc.stage)
		}
	}
}

// -------------------------------------------------------------------------
// RenalDosing Invoke tests
// -------------------------------------------------------------------------

func TestDosingRenal_MetforminHoldLowEGFR(t *testing.T) {
	tool := RenalDosing{}
	// High creatinine → low eGFR → hold metformin
	args, _ := json.Marshal(map[string]interface{}{
		"drug":             "metformin",
		"standard_dose_mg": 500,
		"creatinine_mg_dl": 4.0, // very high → eGFR well below 30
		"age":              65,
		"weight_kg":        70,
		"sex":              "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.AdjustedDoseMg != 0 {
		t.Errorf("expected 0 (hold), got %v", out.AdjustedDoseMg)
	}
	if out.Frequency != "hold" {
		t.Errorf("expected frequency=hold, got %q", out.Frequency)
	}
}

func TestDosingRenal_MetforminNormalEGFR(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]interface{}{
		"drug":             "metformin",
		"standard_dose_mg": 500,
		"creatinine_mg_dl": 0.9,
		"age":              40,
		"weight_kg":        80,
		"sex":              "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.AdjustedDoseMg != 500 {
		t.Errorf("expected 500 mg (no change), got %v", out.AdjustedDoseMg)
	}
}

func TestDosingRenal_Gabapentin50PercentReduction(t *testing.T) {
	tool := RenalDosing{}
	// Target eGFR 30-60: creatinine ~2.4 for 65yo 70kg male
	// eGFR = (75*70)/(72*2.4) = 5250/172.8 ≈ 30.4
	args, _ := json.Marshal(map[string]interface{}{
		"drug":             "gabapentin",
		"standard_dose_mg": 300,
		"creatinine_mg_dl": 2.4,
		"age":              65,
		"weight_kg":        70,
		"sex":              "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// eGFR ≈ 30.4 → between 30-60 → 50% reduction → 150 mg
	if out.AdjustedDoseMg != 150 {
		t.Errorf("expected 150 mg (50%% reduction), got %v mg (eGFR=%.1f)", out.AdjustedDoseMg, out.EgfrMlMin)
	}
}

func TestDosingRenal_Gabapentin75PercentReduction(t *testing.T) {
	tool := RenalDosing{}
	// eGFR < 30: creatinine ~5.0 for 65yo 70kg male
	args, _ := json.Marshal(map[string]interface{}{
		"drug":             "gabapentin",
		"standard_dose_mg": 300,
		"creatinine_mg_dl": 5.0,
		"age":              65,
		"weight_kg":        70,
		"sex":              "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 75% reduction → 75 mg
	if out.AdjustedDoseMg != 75 {
		t.Errorf("expected 75 mg (75%% reduction), got %v mg (eGFR=%.1f)", out.AdjustedDoseMg, out.EgfrMlMin)
	}
}

func TestDosingRenal_LisinoprilLowEGFR(t *testing.T) {
	tool := RenalDosing{}
	// eGFR < 30
	args, _ := json.Marshal(map[string]interface{}{
		"drug":             "lisinopril",
		"standard_dose_mg": 10,
		"creatinine_mg_dl": 5.0,
		"age":              65,
		"weight_kg":        70,
		"sex":              "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.AdjustedDoseMg != 2.5 {
		t.Errorf("expected 2.5 mg starting dose, got %v", out.AdjustedDoseMg)
	}
}

func TestDosingRenal_VancomycinExtendedInterval(t *testing.T) {
	tool := RenalDosing{}
	// eGFR 10-50 → q24h
	args, _ := json.Marshal(map[string]interface{}{
		"drug":             "vancomycin",
		"standard_dose_mg": 1000,
		"creatinine_mg_dl": 3.5,
		"age":              60,
		"weight_kg":        75,
		"sex":              "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Frequency != "q24h" {
		t.Errorf("expected q24h, got %q (eGFR=%.1f)", out.Frequency, out.EgfrMlMin)
	}
}

func TestDosingRenal_DigoxinDoseReduction(t *testing.T) {
	tool := RenalDosing{}
	// eGFR < 50 → reduce 25-50% (implementation: 37.5% reduction → 62.5% of original)
	args, _ := json.Marshal(map[string]interface{}{
		"drug":             "digoxin",
		"standard_dose_mg": 0.25,
		"creatinine_mg_dl": 2.5,
		"age":              70,
		"weight_kg":        65,
		"sex":              "female",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	expected := math.Round(0.25*0.625*100) / 100
	if out.AdjustedDoseMg != expected {
		t.Errorf("expected %.4f mg, got %v (eGFR=%.1f)", expected, out.AdjustedDoseMg, out.EgfrMlMin)
	}
}

func TestDosingRenal_UnknownDrug(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]interface{}{
		"drug":             "fictionalcillin",
		"standard_dose_mg": 500,
		"creatinine_mg_dl": 1.0,
		"age":              40,
		"weight_kg":        70,
		"sex":              "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Unknown drug: returns standard dose with a note
	if out.AdjustedDoseMg != 500 {
		t.Errorf("expected standard dose 500 for unknown drug, got %v", out.AdjustedDoseMg)
	}
}

func TestDosingRenal_EmptyArgs(t *testing.T) {
	tool := RenalDosing{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
}

func TestDosingRenal_InvalidAge(t *testing.T) {
	tool := RenalDosing{}
	args := json.RawMessage(`{"drug":"metformin","standard_dose_mg":500,"creatinine_mg_dl":1.0,"age":0,"weight_kg":70,"sex":"male"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for age=0")
	}
}

// -------------------------------------------------------------------------
// PediatricDosing tests
// -------------------------------------------------------------------------

func TestDosingPediatric_Amoxicillin(t *testing.T) {
	tool := PediatricDosing{}
	args := json.RawMessage(`{"drug":"amoxicillin","weight_kg":20,"age_months":36}`)
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out pediatricDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 15 mg/kg * 20 kg = 300 mg per dose
	if out.DoseMg != 300 {
		t.Errorf("expected 300 mg, got %v", out.DoseMg)
	}
	// max daily: 90 mg/kg * 20 kg = 1800 mg
	if out.MaxDailyMg != 1800 {
		t.Errorf("expected max daily 1800 mg, got %v", out.MaxDailyMg)
	}
	if out.Frequency != "q8h" {
		t.Errorf("expected q8h, got %q", out.Frequency)
	}
}

func TestDosingPediatric_Ibuprofen(t *testing.T) {
	tool := PediatricDosing{}
	args := json.RawMessage(`{"drug":"ibuprofen","weight_kg":25,"age_months":60}`)
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out pediatricDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 7.5 mg/kg * 25 = 187.5 mg/dose
	if out.DoseMg != 187.5 {
		t.Errorf("expected 187.5 mg, got %v", out.DoseMg)
	}
	// max: 40 * 25 = 1000 mg/day
	if out.MaxDailyMg != 1000 {
		t.Errorf("expected max daily 1000 mg, got %v", out.MaxDailyMg)
	}
}

func TestDosingPediatric_Acetaminophen(t *testing.T) {
	tool := PediatricDosing{}
	args := json.RawMessage(`{"drug":"acetaminophen","weight_kg":15,"age_months":30}`)
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out pediatricDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 12.5 * 15 = 187.5 mg/dose
	if out.DoseMg != 187.5 {
		t.Errorf("expected 187.5 mg, got %v", out.DoseMg)
	}
	// max: 75 * 15 = 1125 mg/day
	if out.MaxDailyMg != 1125 {
		t.Errorf("expected 1125 mg max daily, got %v", out.MaxDailyMg)
	}
}

func TestDosingPediatric_Azithromycin(t *testing.T) {
	tool := PediatricDosing{}
	args := json.RawMessage(`{"drug":"azithromycin","weight_kg":20,"age_months":48}`)
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out pediatricDosingOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Day 1: 10 mg/kg * 20 = 200 mg
	if out.DoseMg != 200 {
		t.Errorf("expected 200 mg day-1 dose, got %v", out.DoseMg)
	}
	if out.MaxDailyMg != 200 {
		t.Errorf("expected max daily 200 mg, got %v", out.MaxDailyMg)
	}
}

func TestDosingPediatric_UnknownDrug(t *testing.T) {
	tool := PediatricDosing{}
	args := json.RawMessage(`{"drug":"unicornomycin","weight_kg":20,"age_months":48}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for unknown drug, got nil")
	}
}

func TestDosingPediatric_EmptyArgs(t *testing.T) {
	tool := PediatricDosing{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
}

func TestDosingPediatric_NegativeWeight(t *testing.T) {
	tool := PediatricDosing{}
	args := json.RawMessage(`{"drug":"ibuprofen","weight_kg":-5,"age_months":24}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for negative weight")
	}
}

// -------------------------------------------------------------------------
// Interface compliance: all three tools implement Tool
// -------------------------------------------------------------------------

func TestDosingToolInterfaces(t *testing.T) {
	tools := []Tool{
		WeightBasedDosing{},
		RenalDosing{},
		PediatricDosing{},
	}
	for _, tool := range tools {
		if tool.Name() == "" {
			t.Errorf("tool Name() is empty for %T", tool)
		}
		if tool.Description() == "" {
			t.Errorf("tool Description() is empty for %T", tool)
		}
		var schemaObj map[string]interface{}
		if err := json.Unmarshal(tool.Schema(), &schemaObj); err != nil {
			t.Errorf("tool Schema() is not valid JSON for %T: %v", tool, err)
		}
	}
}

// -------------------------------------------------------------------------
// Registry integration
// -------------------------------------------------------------------------

func TestDosingRegistryIntegration(t *testing.T) {
	reg := NewRegistry(
		WeightBasedDosing{},
		RenalDosing{},
		PediatricDosing{},
	)
	names := []string{"pediatric_dose", "renal_dose_adjust", "weight_based_dose"}
	for _, name := range names {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("tool %q not found in registry", name)
		}
	}
	llmTools := reg.LLMTools()
	if len(llmTools) != 3 {
		t.Errorf("expected 3 LLM tools, got %d", len(llmTools))
	}
}
