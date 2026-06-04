package agents

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func TestIsTroponinElevatedMissing(t *testing.T) {
	state := medical.CaseState{TestResults: map[string]string{}}
	if isTroponinElevated(state) {
		t.Error("missing troponin should not be elevated")
	}
}

func TestIsTroponinElevatedByKeyword(t *testing.T) {
	state := medical.CaseState{TestResults: map[string]string{
		"troponin_i": "Elevated 5.2 ng/mL",
	}}
	if !isTroponinElevated(state) {
		t.Error("'elevated' keyword should be detected")
	}
}

func TestIsTroponinElevatedByNumber(t *testing.T) {
	// The marker-after semantics: extractFirstNumber looks for the first number
	// AFTER the "ng/ml" marker. Use a format where the value follows the marker.
	state := medical.CaseState{TestResults: map[string]string{
		"troponin_i": "reference ng/mL 0.08",
	}}
	if !isTroponinElevated(state) {
		t.Error("numeric > 0.04 should be elevated")
	}
}

func TestIsTroponinNotElevated(t *testing.T) {
	state := medical.CaseState{TestResults: map[string]string{
		"troponin_i": "Troponin I 0.02 ng/mL",
	}}
	if isTroponinElevated(state) {
		t.Error("numeric <= 0.04 should not be elevated")
	}
}

func TestIsTroponinUndetectable(t *testing.T) {
	state := medical.CaseState{TestResults: map[string]string{
		"troponin_i": "Undetectable",
	}}
	if isTroponinElevated(state) {
		t.Error("undetectable should not be elevated")
	}
}

func TestIsPELikelyCTPA(t *testing.T) {
	state := medical.CaseState{TestResults: map[string]string{
		"ct_pulmonary_angiography": "Bilateral filling defects",
	}}
	if !isPELikely(state) {
		t.Error("CTPA with filling defect should be PE-likely")
	}
}

func TestIsPELikelyDDimerPlusTachycardia(t *testing.T) {
	state := medical.CaseState{
		TestResults: map[string]string{
			"d_dimer": "D-dimer 3400 ng/mL FEU (markedly elevated)",
		},
		Presentation: "HR 120 bpm, dyspnea",
	}
	if !isPELikely(state) {
		t.Error("elevated d-dimer + tachycardia should be PE-likely")
	}
}

func TestIsPELikelyDDimerNumericPlusTachycardia(t *testing.T) {
	state := medical.CaseState{
		TestResults: map[string]string{
			"d_dimer": "D-dimer 800 units",
		},
		Presentation: "heart rate 115, dyspnea",
	}
	if !isPELikely(state) {
		t.Error("d-dimer numeric > 500 + tachycardia should be PE-likely")
	}
}

func TestIsPELikelyTachycardiaPlusHypoxemia(t *testing.T) {
	state := medical.CaseState{
		Presentation: "HR 110, SpO2 90%",
	}
	if !isPELikely(state) {
		t.Error("tachycardia + hypoxemia should be PE-likely")
	}
}

func TestIsPELikelyConditionTag(t *testing.T) {
	state := medical.CaseState{Condition: "pe"}
	if !isPELikely(state) {
		t.Error("condition=pe should be PE-likely")
	}
}

func TestIsPELikelyNotLikely(t *testing.T) {
	state := medical.CaseState{
		Condition:   "cap",
		TestResults: map[string]string{},
	}
	if isPELikely(state) {
		t.Error("no PE signals should not be PE-likely")
	}
}

func TestIsTachycardic(t *testing.T) {
	cases := []struct {
		pres string
		want bool
	}{
		{"", false},
		{"HR 120 bpm", true},
		{"heart rate 115", true},
		{"HR 80 bpm", false},
		{"no vitals", false},
		{"Heart Rate 105", true},
	}
	for _, tc := range cases {
		got := isTachycardic(tc.pres)
		if got != tc.want {
			t.Errorf("isTachycardic(%q)=%v, want %v", tc.pres, got, tc.want)
		}
	}
}

func TestIsHypoxemic(t *testing.T) {
	cases := []struct {
		pres string
		want bool
	}{
		{"", false},
		{"SpO2 90%", true},
		{"spo2 88", true},
		{"SpO2 98%", false},
		{"no vitals", false},
	}
	for _, tc := range cases {
		got := isHypoxemic(tc.pres)
		if got != tc.want {
			t.Errorf("isHypoxemic(%q)=%v, want %v", tc.pres, got, tc.want)
		}
	}
}

func TestExtractFirstNumberEmptyString(t *testing.T) {
	_, ok := extractFirstNumber("", "marker")
	if ok {
		t.Error("empty string should return false")
	}
}

func TestExtractFirstNumberNoMarkerMatch(t *testing.T) {
	_, ok := extractFirstNumber("some text 42", "missing_marker")
	if ok {
		t.Error("missing marker should return false")
	}
}

func TestExtractFirstNumberMarkerNoNumber(t *testing.T) {
	_, ok := extractFirstNumber("glucose no-number-here", "glucose")
	if ok {
		t.Error("marker with no trailing number should return false")
	}
}

func TestExtractWBCNoMarker(t *testing.T) {
	_, ok := extractWBC("no wbc data")
	if ok {
		t.Error("missing WBC marker should return false")
	}
}

func TestExtractWBCNoNumber(t *testing.T) {
	_, ok := extractWBC("WBC pending")
	if ok {
		t.Error("WBC with no number should return false")
	}
}

func TestInferDiagnosisPE(t *testing.T) {
	state := medical.CaseState{
		Condition: "pe",
		TestResults: map[string]string{
			"ct_pulmonary_angiography": "Bilateral pulmonary arterial filling defects",
			"d_dimer":                  "D-dimer 3400 ng/mL FEU (markedly elevated)",
		},
	}
	got := inferDiagnosis(state)
	if got.Diagnosis != "pulmonary_embolism" {
		t.Errorf("diagnosis=%q, want pulmonary_embolism", got.Diagnosis)
	}
	if got.Confidence < 0.7 {
		t.Errorf("confidence=%.2f, want >= 0.7", got.Confidence)
	}
}

func TestDiagnosticianHandleMessageEmptyContent(t *testing.T) {
	a := &DiagnosticianAgent{}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", "", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on empty content")
	}
}

func TestDiagnosticianHandleMessageInvalidJSON(t *testing.T) {
	a := &DiagnosticianAgent{}
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent,
		"case_state", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestExtractFirstNumberNoMarker(t *testing.T) {
	// No marker: should extract first number from whole string.
	val, ok := extractFirstNumber("42 is the answer", "")
	if !ok || val != 42 {
		t.Errorf("got %v ok=%v, want 42 ok=true", val, ok)
	}
}

func TestIsPELikelyDDimerNotElevatedNoTachycardia(t *testing.T) {
	state := medical.CaseState{
		TestResults: map[string]string{
			"d_dimer": "D-dimer 200 ng/mL",
		},
		Presentation: "HR 70 bpm",
	}
	if isPELikely(state) {
		t.Error("low d-dimer without tachycardia should not be PE-likely")
	}
}

func TestIsPELikelyDDimerElevatedNoTachycardia(t *testing.T) {
	state := medical.CaseState{
		TestResults: map[string]string{
			"d_dimer": "elevated d-dimer",
		},
		Presentation: "HR 70 bpm, normal vitals",
	}
	if isPELikely(state) {
		t.Error("elevated d-dimer without tachycardia should not be PE-likely (missing HR>100)")
	}
}
