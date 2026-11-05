package safety

import (
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/knowledge"
)

func TestIsRefusal_DetectsCommonPhrases(t *testing.T) {
	cases := []string{
		"I cannot provide a diagnosis without seeing the patient.",
		"I can't provide medical advice in this context.",
		"I'm unable to assist with that request.",
		"I am not able to give a clinical recommendation.",
		"I cannot diagnose this remotely.",
		"As an AI language model, I cannot make decisions.",
		"I'm just an AI and not a doctor.",
		"Please consult a healthcare professional for guidance.",
		"I'm not qualified to comment on this.",
	}
	if len(cases) < 5 {
		t.Fatalf("test setup: need at least 5 phrases")
	}
	for _, c := range cases {
		if !IsRefusal(c) {
			t.Errorf("IsRefusal(%q) = false; want true", c)
		}
	}
}

func TestIsRefusal_NoFalsePositiveOnClinicalText(t *testing.T) {
	clinical := []string{
		"diagnosis: pneumonia. confidence 0.87",
		"Patient presents with fever and productive cough; recommend chest X-ray.",
		"Plan: start amoxicillin 500mg TID for 7 days.",
	}
	for _, c := range clinical {
		if IsRefusal(c) {
			t.Errorf("IsRefusal(%q) = true; want false", c)
		}
	}
}

func TestCheckHallucination_GroundedClaimPasses(t *testing.T) {
	claim := "Pneumonia typically presents with productive cough and fever."
	cites := []knowledge.Citation{
		{Title: "Community-acquired pneumonia overview", Snippet: "Pneumonia commonly presents with productive cough, fever, and pleuritic chest pain."},
	}
	v := CheckHallucination(claim, cites)
	if !v.OK {
		t.Errorf("expected grounded claim to pass, got Verdict{OK:false, Reason:%q}", v.Reason)
	}
}

func TestCheckHallucination_NoCitationsFails(t *testing.T) {
	v := CheckHallucination("Pneumonia presents with productive cough", nil)
	if v.OK {
		t.Errorf("expected OK:false for empty citations, got OK:true")
	}
	if v.Reason == "" {
		t.Errorf("expected a non-empty Reason on failure")
	}
}

func TestCheckHallucination_EmptyClaimPasses(t *testing.T) {
	// Only stopwords and short tokens — no significant terms.
	v := CheckHallucination("the and for", nil)
	if !v.OK {
		t.Errorf("expected OK:true for claim with no significant terms, got Reason:%q", v.Reason)
	}
}

func TestCheckHallucinationThreshold_Respected(t *testing.T) {
	claim := "Pneumonia presents with productive cough and fever and chills tonight."
	cites := []knowledge.Citation{
		{Title: "Notes", Snippet: "Pneumonia productive cough."},
	}
	// Should pass at low threshold (some terms hit).
	if v := CheckHallucinationThreshold(claim, cites, 0.3); !v.OK {
		t.Errorf("expected pass at threshold 0.3, got Reason:%q", v.Reason)
	}
	// Should fail at strict threshold.
	if v := CheckHallucinationThreshold(claim, cites, 0.9); v.OK {
		t.Errorf("expected fail at threshold 0.9, got OK:true")
	}
}

func TestContainsPHIPatterns_DetectsSSN_Phone_MRN(t *testing.T) {
	cases := map[string]string{
		"ssn":   "Patient SSN is 123-45-6789, please redact.",
		"phone": "Reach patient at 415-555-1234 after 5pm.",
		"mrn":   "Reference MRN: 1234567 in the chart.",
	}
	for label, txt := range cases {
		v := ContainsPHIPatterns(txt)
		if v.OK {
			t.Errorf("expected PHI detection for %s case in %q, got OK:true", label, txt)
		}
		if !strings.Contains(v.Reason, "phi pattern") {
			t.Errorf("expected reason to name pattern for %s case, got %q", label, v.Reason)
		}
	}
}

func TestContainsPHIPatterns_CleanTextPasses(t *testing.T) {
	clean := []string{
		"Patient presents with cough and fever.",
		"Plan: amoxicillin 500mg TID for 7 days.",
		"Lab value 4-5 mmol/L within range.",
	}
	for _, c := range clean {
		v := ContainsPHIPatterns(c)
		if !v.OK {
			t.Errorf("expected clean text %q to pass, got Reason:%q", c, v.Reason)
		}
	}
}
