package medical

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

// ---------------------------------------------------------------------------
// Vitals extraction
// ---------------------------------------------------------------------------

func TestScribeExtractVitals_BPSlash(t *testing.T) {
	v := ExtractVitalsFromText("BP 120/80 noted on admission")
	if v.BP != "120/80" {
		t.Errorf("BP: got %q, want %q", v.BP, "120/80")
	}
}

func TestScribeExtractVitals_BPNatural(t *testing.T) {
	v := ExtractVitalsFromText("blood pressure 130 over 85")
	if v.BP != "130/85" {
		t.Errorf("BP: got %q, want %q", v.BP, "130/85")
	}
}

func TestScribeExtractVitals_HeartRate(t *testing.T) {
	v := ExtractVitalsFromText("heart rate 88 regular")
	if v.HR != 88 {
		t.Errorf("HR: got %d, want %d", v.HR, 88)
	}
}

func TestScribeExtractVitals_HR(t *testing.T) {
	v := ExtractVitalsFromText("HR 72")
	if v.HR != 72 {
		t.Errorf("HR: got %d, want %d", v.HR, 72)
	}
}

func TestScribeExtractVitals_Pulse(t *testing.T) {
	v := ExtractVitalsFromText("pulse 96")
	if v.HR != 96 {
		t.Errorf("HR from pulse: got %d, want %d", v.HR, 96)
	}
}

func TestScribeExtractVitals_TempFahrenheit(t *testing.T) {
	v := ExtractVitalsFromText("temperature 101.2")
	// 101.2F = 38.4C
	if v.Temp < 38.0 || v.Temp > 39.0 {
		t.Errorf("Temp (F->C): got %.1f, want ~38.4", v.Temp)
	}
}

func TestScribeExtractVitals_TempCelsius(t *testing.T) {
	v := ExtractVitalsFromText("temp 38.5")
	if v.Temp != 38.5 {
		t.Errorf("Temp: got %.1f, want 38.5", v.Temp)
	}
}

func TestScribeExtractVitals_RespiratoryRate(t *testing.T) {
	v := ExtractVitalsFromText("respiratory rate 18")
	if v.RR != 18 {
		t.Errorf("RR: got %d, want %d", v.RR, 18)
	}
}

func TestScribeExtractVitals_RR(t *testing.T) {
	v := ExtractVitalsFromText("RR 22")
	if v.RR != 22 {
		t.Errorf("RR: got %d, want %d", v.RR, 22)
	}
}

func TestScribeExtractVitals_SpO2(t *testing.T) {
	v := ExtractVitalsFromText("SpO2 95%")
	if v.SpO2 != 95 {
		t.Errorf("SpO2: got %.0f, want 95", v.SpO2)
	}
}

func TestScribeExtractVitals_OxygenSat(t *testing.T) {
	v := ExtractVitalsFromText("oxygen sat 97")
	if v.SpO2 != 97 {
		t.Errorf("SpO2: got %.0f, want 97", v.SpO2)
	}
}

func TestScribeExtractVitals_PulseOx(t *testing.T) {
	v := ExtractVitalsFromText("pulse ox 92")
	if v.SpO2 != 92 {
		t.Errorf("SpO2: got %.0f, want 92", v.SpO2)
	}
}

func TestScribeExtractVitals_Full(t *testing.T) {
	text := "Patient presents with BP 140/90, heart rate 100, temp 102.0, RR 24, SpO2 93%"
	v := ExtractVitalsFromText(text)

	if v.BP != "140/90" {
		t.Errorf("BP: got %q, want %q", v.BP, "140/90")
	}
	if v.HR != 100 {
		t.Errorf("HR: got %d, want %d", v.HR, 100)
	}
	// 102.0F = 38.9C
	if v.Temp < 38.5 || v.Temp > 39.5 {
		t.Errorf("Temp: got %.1f, want ~38.9", v.Temp)
	}
	if v.RR != 24 {
		t.Errorf("RR: got %d, want %d", v.RR, 24)
	}
	if v.SpO2 != 93 {
		t.Errorf("SpO2: got %.0f, want 93", v.SpO2)
	}
}

// ---------------------------------------------------------------------------
// Medication extraction
// ---------------------------------------------------------------------------

func TestScribeExtractMedications(t *testing.T) {
	text := "Patient takes metformin 500mg twice daily, lisinopril 10mg, and aspirin 81mg"
	meds := ExtractMedicationsFromText(text)

	want := map[string]bool{"metformin": true, "lisinopril": true, "aspirin": true}
	got := make(map[string]bool)
	for _, m := range meds {
		got[m] = true
	}

	for w := range want {
		if !got[w] {
			t.Errorf("missing medication %q in %v", w, meds)
		}
	}
}

func TestScribeExtractMedications_NoMeds(t *testing.T) {
	meds := ExtractMedicationsFromText("Patient denies any medications")
	if len(meds) != 0 {
		t.Errorf("expected no medications, got %v", meds)
	}
}

func TestScribeExtractMedications_CaseInsensitive(t *testing.T) {
	meds := ExtractMedicationsFromText("Currently on Metformin and ATORVASTATIN")
	if len(meds) != 2 {
		t.Errorf("expected 2 medications, got %v", meds)
	}
}

// ---------------------------------------------------------------------------
// Allergy extraction
// ---------------------------------------------------------------------------

func TestScribeExtractAllergies_Single(t *testing.T) {
	allergies := ExtractAllergiesFromText("Patient is allergic to penicillin")
	if len(allergies) != 1 || allergies[0] != "penicillin" {
		t.Errorf("expected [penicillin], got %v", allergies)
	}
}

func TestScribeExtractAllergies_Multiple(t *testing.T) {
	allergies := ExtractAllergiesFromText("Allergic to penicillin, allergy to sulfa drugs")
	if len(allergies) != 2 {
		t.Errorf("expected 2 allergies, got %v", allergies)
	}
}

func TestScribeExtractAllergies_NKDA(t *testing.T) {
	allergies := ExtractAllergiesFromText("NKDA")
	if len(allergies) != 1 || allergies[0] != "NKDA" {
		t.Errorf("expected [NKDA], got %v", allergies)
	}
}

func TestScribeExtractAllergies_NoKnownDrugAllergies(t *testing.T) {
	allergies := ExtractAllergiesFromText("No known drug allergies")
	if len(allergies) != 1 || allergies[0] != "NKDA" {
		t.Errorf("expected [NKDA], got %v", allergies)
	}
}

// ---------------------------------------------------------------------------
// RuleBasedScribe end-to-end
// ---------------------------------------------------------------------------

func TestScribeRuleBasedFull(t *testing.T) {
	transcript := `Patient is a 65-year-old male presenting with chest pain and shortness of breath. He reports the pain started two hours ago while climbing stairs. Blood pressure 150/95, heart rate 110, respiratory rate 24, temperature 98.6, SpO2 94%. Currently on metoprolol 50mg daily and aspirin 81mg daily. Patient is allergic to penicillin.`

	out := RuleBasedScribe(ScribeInput{
		Transcript: transcript,
		PatientID:  "P001",
		CaseID:     "C001",
	})

	if out.Method != "rule" {
		t.Errorf("Method: got %q, want %q", out.Method, "rule")
	}
	if out.ChiefComplaint == "" {
		t.Error("ChiefComplaint should not be empty")
	}
	if out.HPI == "" {
		t.Error("HPI should not be empty")
	}
	if out.Vitals.BP != "150/95" {
		t.Errorf("BP: got %q, want %q", out.Vitals.BP, "150/95")
	}
	if out.Vitals.HR != 110 {
		t.Errorf("HR: got %d, want %d", out.Vitals.HR, 110)
	}
	if out.Vitals.RR != 24 {
		t.Errorf("RR: got %d, want %d", out.Vitals.RR, 24)
	}
	if out.Vitals.SpO2 != 94 {
		t.Errorf("SpO2: got %.0f, want 94", out.Vitals.SpO2)
	}
	if len(out.Medications) < 2 {
		t.Errorf("expected at least 2 medications, got %v", out.Medications)
	}
	if len(out.Allergies) == 0 {
		t.Error("expected at least one allergy")
	}
	if out.Confidence <= 0 {
		t.Error("Confidence should be > 0")
	}
}

func TestScribeRuleBasedEmpty(t *testing.T) {
	out := RuleBasedScribe(ScribeInput{Transcript: ""})
	if out.Confidence != 0 {
		t.Errorf("Confidence for empty: got %f, want 0", out.Confidence)
	}
	if out.Method != "rule" {
		t.Errorf("Method: got %q, want %q", out.Method, "rule")
	}
}

// ---------------------------------------------------------------------------
// LLMScribe with mock client
// ---------------------------------------------------------------------------

func TestScribeLLMSuccess(t *testing.T) {
	mockOutput := ScribeOutput{
		ChiefComplaint: "chest pain",
		HPI:            "2-hour history of substernal chest pain",
		Vitals:         ExtractedVitals{HR: 110, BP: "150/95"},
		Medications:    []string{"metoprolol", "aspirin"},
		Allergies:      []string{"penicillin"},
		Confidence:     0.9,
		Method:         "llm",
	}
	respBytes, _ := json.Marshal(mockOutput)

	mock := &llm.Mock{
		Resp: &llm.Response{
			Content:  string(respBytes),
			Provider: "mock",
			Model:    "mock-model",
		},
	}

	out, err := LLMScribe(context.Background(), mock, ScribeInput{
		Transcript: "chest pain for 2 hours",
		PatientID:  "P001",
		CaseID:     "C001",
	})
	if err != nil {
		t.Fatalf("LLMScribe: unexpected error: %v", err)
	}
	if out.Method != "llm" {
		t.Errorf("Method: got %q, want %q", out.Method, "llm")
	}
	if out.ChiefComplaint != "chest pain" {
		t.Errorf("ChiefComplaint: got %q, want %q", out.ChiefComplaint, "chest pain")
	}
	if len(mock.Calls) != 1 {
		t.Errorf("expected 1 LLM call, got %d", len(mock.Calls))
	}
}

func TestScribeLLMFallback(t *testing.T) {
	// Simulate LLM returning invalid JSON — should fall back to rule-based.
	mock := &llm.Mock{
		Resp: &llm.Response{
			Content: "this is not json at all",
		},
	}

	out, err := LLMScribe(context.Background(), mock, ScribeInput{
		Transcript: "Patient presents with BP 120/80 and heart rate 72",
	})
	if err != nil {
		t.Fatalf("LLMScribe fallback: unexpected error: %v", err)
	}
	if out.Method != "rule" {
		t.Errorf("expected rule fallback, got method=%q", out.Method)
	}
	if out.Vitals.BP != "120/80" {
		t.Errorf("fallback BP: got %q, want %q", out.Vitals.BP, "120/80")
	}
}

func TestScribeLLMCodeFences(t *testing.T) {
	// Simulate LLM wrapping JSON in markdown code fences.
	mockOutput := ScribeOutput{
		ChiefComplaint: "headache",
		Method:         "llm",
		Confidence:     0.8,
	}
	respBytes, _ := json.Marshal(mockOutput)
	fenced := "```json\n" + string(respBytes) + "\n```"

	mock := &llm.Mock{
		Resp: &llm.Response{Content: fenced},
	}

	out, err := LLMScribe(context.Background(), mock, ScribeInput{Transcript: "headache"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ChiefComplaint != "headache" {
		t.Errorf("ChiefComplaint: got %q, want %q", out.ChiefComplaint, "headache")
	}
}

// ---------------------------------------------------------------------------
// AutoScribe
// ---------------------------------------------------------------------------

func TestScribeAutoNilClient(t *testing.T) {
	out, err := AutoScribe(context.Background(), nil, ScribeInput{
		Transcript: "BP 130/85, heart rate 88",
	})
	if err != nil {
		t.Fatalf("AutoScribe nil client: unexpected error: %v", err)
	}
	if out.Method != "rule" {
		t.Errorf("Method: got %q, want %q", out.Method, "rule")
	}
}

func TestScribeAutoWithClient(t *testing.T) {
	mockOutput := ScribeOutput{
		ChiefComplaint: "cough",
		Method:         "llm",
		Confidence:     0.85,
	}
	respBytes, _ := json.Marshal(mockOutput)

	mock := &llm.Mock{
		Resp: &llm.Response{Content: string(respBytes)},
	}

	out, err := AutoScribe(context.Background(), mock, ScribeInput{
		Transcript: "persistent cough for 3 weeks",
	})
	if err != nil {
		t.Fatalf("AutoScribe: unexpected error: %v", err)
	}
	if out.Method != "llm" {
		t.Errorf("Method: got %q, want %q", out.Method, "llm")
	}
}

// ---------------------------------------------------------------------------
// stripCodeFences
// ---------------------------------------------------------------------------

func TestScribeStripCodeFences(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{`{"a":1}`, `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```\n{\"a\":1}\n```", `{"a":1}`},
		{"  ```json\n{\"a\":1}\n```  ", `{"a":1}`},
	}
	for _, tt := range tests {
		got := stripCodeFences(tt.in)
		if got != tt.want {
			t.Errorf("stripCodeFences(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
