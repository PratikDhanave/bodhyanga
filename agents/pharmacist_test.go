package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// makeReviewMsg builds a medication_review_request message for a given
// patient ID and medication list.
func makeReviewMsg(patientID string, meds []medicationInput) agent.Message {
	body, _ := json.Marshal(medicationReviewRequest{
		PatientID:   patientID,
		CaseID:      "case-test-01",
		Medications: meds,
	})
	return agent.NewMessage("user", "pharmacist", agent.RoleUser,
		"medication_review_request", string(body),
		map[string]any{"patient_id": patientID, "case_id": "case-test-01"})
}

// decodeResult unmarshals the first output message's Content into
// medicationReviewResult, failing the test if anything goes wrong.
func decodeResult(t *testing.T, msgs []agent.Message) medicationReviewResult {
	t.Helper()
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 output message, got %d", len(msgs))
	}
	if msgs[0].Type != "medication_review_complete" {
		t.Fatalf("expected Type=medication_review_complete, got %q", msgs[0].Type)
	}
	var r medicationReviewResult
	if err := json.Unmarshal([]byte(msgs[0].Content), &r); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return r
}

func TestPharmacistNoInteractions(t *testing.T) {
	a := &PharmacistAgent{}
	meds := []medicationInput{
		{Name: "Amoxicillin", Dose: "500 mg", Frequency: "TID"},
		{Name: "Loratadine", Dose: "10 mg", Frequency: "daily"},
	}
	msgs, err := a.HandleMessage(context.Background(), makeReviewMsg("p-safe", meds), newFakeEnv())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := decodeResult(t, msgs)
	if !r.Safe {
		t.Errorf("expected safe=true for non-interacting meds, got false; interactions=%+v", r.Interactions)
	}
	if len(r.Interactions) != 0 {
		t.Errorf("expected 0 interactions, got %d: %+v", len(r.Interactions), r.Interactions)
	}
}

func TestPharmacistKnownInteractions(t *testing.T) {
	tests := []struct {
		name          string
		meds          []medicationInput
		wantSafe      bool
		wantSeverity  string
		wantDrugA     string
		wantDrugB     string
	}{
		{
			name: "warfarin + aspirin → HIGH bleeding risk",
			meds: []medicationInput{
				{Name: "warfarin", Dose: "5 mg"},
				{Name: "aspirin", Dose: "81 mg"},
			},
			wantSafe:     false,
			wantSeverity: "HIGH",
			wantDrugA:    "warfarin",
			wantDrugB:    "aspirin",
		},
		{
			name: "warfarin + ibuprofen → HIGH (NSAID path)",
			meds: []medicationInput{
				{Name: "warfarin", Dose: "5 mg"},
				{Name: "ibuprofen", Dose: "400 mg"},
			},
			wantSafe:     false,
			wantSeverity: "HIGH",
			wantDrugA:    "warfarin",
			wantDrugB:    "ibuprofen",
		},
		{
			name: "lisinopril + potassium → HIGH hyperkalemia",
			meds: []medicationInput{
				{Name: "lisinopril", Dose: "10 mg"},
				{Name: "potassium", Dose: "20 mEq"},
			},
			wantSafe:     false,
			wantSeverity: "HIGH",
			wantDrugA:    "lisinopril",
			wantDrugB:    "potassium",
		},
		{
			name: "metformin + contrast dye → MODERATE lactic acidosis note",
			meds: []medicationInput{
				{Name: "metformin", Dose: "500 mg"},
				{Name: "contrast dye"},
			},
			wantSafe:     false,
			wantSeverity: "MODERATE",
			wantDrugA:    "metformin",
			wantDrugB:    "contrast dye",
		},
		{
			name: "digoxin + amiodarone → HIGH toxicity",
			meds: []medicationInput{
				{Name: "digoxin", Dose: "0.125 mg"},
				{Name: "amiodarone", Dose: "200 mg"},
			},
			wantSafe:     false,
			wantSeverity: "HIGH",
			wantDrugA:    "digoxin",
			wantDrugB:    "amiodarone",
		},
		{
			name: "simvastatin + amiodarone → MODERATE myopathy",
			meds: []medicationInput{
				{Name: "simvastatin", Dose: "40 mg"},
				{Name: "amiodarone", Dose: "200 mg"},
			},
			wantSafe:     false,
			wantSeverity: "MODERATE",
			wantDrugA:    "simvastatin",
			wantDrugB:    "amiodarone",
		},
		{
			name: "sildenafil + nitroglycerin → CONTRAINDICATED hypotension",
			meds: []medicationInput{
				{Name: "sildenafil", Dose: "50 mg"},
				{Name: "nitroglycerin", Dose: "0.4 mg"},
			},
			wantSafe:     false,
			wantSeverity: "CONTRAINDICATED",
			wantDrugA:    "sildenafil",
			wantDrugB:    "nitroglycerin",
		},
		{
			name: "sertraline + tramadol → HIGH serotonin syndrome",
			meds: []medicationInput{
				{Name: "sertraline", Dose: "50 mg"},
				{Name: "tramadol", Dose: "50 mg"},
			},
			wantSafe:     false,
			wantSeverity: "HIGH",
			wantDrugA:    "sertraline",
			wantDrugB:    "tramadol",
		},
		{
			name: "lithium + naproxen → MODERATE lithium toxicity",
			meds: []medicationInput{
				{Name: "lithium", Dose: "300 mg"},
				{Name: "naproxen", Dose: "500 mg"},
			},
			wantSafe:     false,
			wantSeverity: "MODERATE",
			wantDrugA:    "lithium",
			wantDrugB:    "naproxen",
		},
		{
			name: "haloperidol + azithromycin → HIGH QT / torsades",
			meds: []medicationInput{
				{Name: "haloperidol", Dose: "5 mg"},
				{Name: "azithromycin", Dose: "500 mg"},
			},
			wantSafe:     false,
			wantSeverity: "HIGH",
			wantDrugA:    "haloperidol",
			wantDrugB:    "azithromycin",
		},
		{
			name: "amiodarone + azithromycin → HIGH QT / torsades",
			meds: []medicationInput{
				{Name: "amiodarone", Dose: "200 mg"},
				{Name: "azithromycin", Dose: "500 mg"},
			},
			wantSafe:     false,
			wantSeverity: "HIGH",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &PharmacistAgent{}
			msgs, err := a.HandleMessage(context.Background(), makeReviewMsg("p1", tc.meds), newFakeEnv())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			r := decodeResult(t, msgs)
			if r.Safe != tc.wantSafe {
				t.Errorf("safe: want %v got %v; interactions=%+v", tc.wantSafe, r.Safe, r.Interactions)
			}
			if len(r.Interactions) == 0 {
				t.Fatalf("expected at least one interaction, got none")
			}
			// Find the expected severity in the results.
			found := false
			for _, ix := range r.Interactions {
				if ix.Severity == tc.wantSeverity {
					found = true
					// Only check DrugA/DrugB when the test provides them.
					if tc.wantDrugA != "" && ix.DrugA != tc.wantDrugA {
						t.Errorf("drug_a: want %q got %q", tc.wantDrugA, ix.DrugA)
					}
					if tc.wantDrugB != "" && ix.DrugB != tc.wantDrugB {
						t.Errorf("drug_b: want %q got %q", tc.wantDrugB, ix.DrugB)
					}
					break
				}
			}
			if !found {
				t.Errorf("wanted severity %q in interactions, got %+v", tc.wantSeverity, r.Interactions)
			}
			if len(r.Recommendations) == 0 {
				t.Errorf("expected at least one recommendation for an interaction")
			}
		})
	}
}

// TestPharmacistMultipleInteractions verifies that all applicable rules fire
// when a single prescription list contains several interacting pairs.
func TestPharmacistMultipleInteractions(t *testing.T) {
	a := &PharmacistAgent{}
	meds := []medicationInput{
		{Name: "warfarin", Dose: "5 mg"},
		{Name: "aspirin", Dose: "81 mg"},
		{Name: "digoxin", Dose: "0.125 mg"},
		{Name: "amiodarone", Dose: "200 mg"},
	}
	msgs, err := a.HandleMessage(context.Background(), makeReviewMsg("p-multi", meds), newFakeEnv())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := decodeResult(t, msgs)
	if r.Safe {
		t.Errorf("expected safe=false for multi-interaction list")
	}
	if len(r.Interactions) < 2 {
		t.Errorf("expected at least 2 interactions (warfarin+aspirin, digoxin+amiodarone), got %d: %+v",
			len(r.Interactions), r.Interactions)
	}
}

// TestPharmacistMetadata ensures correlation metadata (patient_id, case_id)
// travels through to the output message.
func TestPharmacistMetadata(t *testing.T) {
	a := &PharmacistAgent{}
	meds := []medicationInput{{Name: "Amoxicillin", Dose: "500 mg"}}
	msg := agent.NewMessage("user", "pharmacist", agent.RoleUser,
		"medication_review_request",
		`{"patient_id":"p99","case_id":"c42","medications":[{"name":"Amoxicillin"}]}`,
		map[string]any{"patient_id": "p99", "case_id": "c42", "tenant_id": "clinic-1"})
	_ = meds

	msgs, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(msgs))
	}
	out := msgs[0]
	if out.Metadata["patient_id"] != "p99" {
		t.Errorf("patient_id propagation: want p99 got %v", out.Metadata["patient_id"])
	}
	if out.Metadata["case_id"] != "c42" {
		t.Errorf("case_id propagation: want c42 got %v", out.Metadata["case_id"])
	}
	if out.Metadata["tenant_id"] != "clinic-1" {
		t.Errorf("tenant_id propagation: want clinic-1 got %v", out.Metadata["tenant_id"])
	}
}

// TestPharmacistInvalidRequest verifies that a malformed Content returns an
// error (and therefore no output messages).
func TestPharmacistInvalidRequest(t *testing.T) {
	a := &PharmacistAgent{}
	bad := agent.NewMessage("user", "pharmacist", agent.RoleUser,
		"medication_review_request", `not json`, nil)
	msgs, err := a.HandleMessage(context.Background(), bad, newFakeEnv())
	if err == nil {
		t.Errorf("expected error for invalid JSON, got nil (msgs=%+v)", msgs)
	}
}
