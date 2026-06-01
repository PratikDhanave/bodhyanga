package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// helpers

func makeScreenRequest(t *testing.T, req TrialScreenRequest) agent.Message {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return agent.NewMessage("test", "trial_screener", agent.RoleUser,
		"trial_screen_request", string(b), nil)
}

func decodeTrialResult(t *testing.T, msgs []agent.Message) TrialScreenResult {
	t.Helper()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 output message, got %d", len(msgs))
	}
	var result TrialScreenResult
	if err := json.Unmarshal([]byte(msgs[0].Content), &result); err != nil {
		t.Fatalf("decode result: %v\nbody=%s", err, msgs[0].Content)
	}
	return result
}

// ---- identity / metadata ----

func TestTrialScreener_Identity(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	if a.ID() != "trial_screener" {
		t.Errorf("ID=%q, want trial_screener", a.ID())
	}
	if a.Name() == "" {
		t.Error("Name() is empty")
	}
	caps := a.Capabilities()
	wantCaps := map[string]bool{"trial_eligibility": false, "research_screening": false}
	for _, c := range caps {
		if _, ok := wantCaps[c]; ok {
			wantCaps[c] = true
		}
	}
	for k, found := range wantCaps {
		if !found {
			t.Errorf("capability %q missing from Capabilities()", k)
		}
	}
}

// ---- output message routing ----

func TestTrialScreener_OutputAddressedToSupervisor(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	msgs, err := a.HandleMessage(context.Background(),
		makeScreenRequest(t, TrialScreenRequest{
			PatientID: "pt-test",
			Diagnosis: "NSCLC",
			Age:       55,
			ECOGScore: 1,
		}),
		newFakeEnv(),
	)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 message, got %d", len(msgs))
	}
	if msgs[0].To != "diagnostic_supervisor" {
		t.Errorf("To=%q, want diagnostic_supervisor", msgs[0].To)
	}
	if msgs[0].Type != "trial_screen_result" {
		t.Errorf("Type=%q, want trial_screen_result", msgs[0].Type)
	}
}

// ---- BODH-001: NSCLC ----

func TestTrialScreener_NSCLCPatientEligibleForBODH001(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-nsclc",
		Diagnosis: "non-small cell lung cancer (NSCLC) Stage IV",
		Age:       62,
		ECOGScore: 1,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	if result.TotalScreened != len(mockTrials) {
		t.Errorf("TotalScreened=%d, want %d", result.TotalScreened, len(mockTrials))
	}

	found := false
	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-001" {
			found = true
			if trial.MatchScore < 0.5 {
				t.Errorf("BODH-001 match_score=%.2f, want >= 0.5", trial.MatchScore)
			}
			// All four criteria should pass for this patient.
			if trial.MatchScore != 1.0 {
				t.Errorf("BODH-001 match_score=%.2f, want 1.0 for qualifying NSCLC patient", trial.MatchScore)
			}
		}
	}
	if !found {
		t.Error("BODH-001 not in eligible_trials for NSCLC patient")
	}
}

func TestTrialScreener_NSCLCWithCheckpointInhibitorExcluded(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID:       "pt-nsclc-pretreated",
		Diagnosis:       "NSCLC Stage IIIB",
		Age:             58,
		ECOGScore:       1,
		PriorTreatments: []string{"pembrolizumab", "carboplatin"},
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-001" && trial.MatchScore == 1.0 {
			t.Error("BODH-001 should not have perfect score for patient with prior checkpoint inhibitor")
		}
	}
}

// ---- BODH-002: HER2+ breast cancer ----

func TestTrialScreener_HER2BreastCancerEligibleForBODH002(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-breast-her2",
		Diagnosis: "HER2+ breast cancer, metastatic",
		Age:       45,
		ECOGScore: 0,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-002" {
			if trial.MatchScore != 1.0 {
				t.Errorf("BODH-002 match_score=%.2f, want 1.0", trial.MatchScore)
			}
			return
		}
	}
	t.Error("BODH-002 not found in eligible trials for HER2+ breast cancer patient")
}

func TestTrialScreener_HER2BreastCancerPoorECOGExcluded(t *testing.T) {
	// ECOG 2 fails BODH-002 (requires 0-1) — score drops to 0.75, still >= 0.5
	// but criteria_failed should contain the ECOG criterion.
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-breast-her2-ecog2",
		Diagnosis: "HER2+ breast cancer",
		Age:       50,
		ECOGScore: 2,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-002" {
			if trial.MatchScore == 1.0 {
				t.Error("BODH-002 should not score 1.0 for ECOG 2 patient (requires 0-1)")
			}
			if len(trial.CriteriaFailed) == 0 {
				t.Error("BODH-002 criteria_failed should be non-empty for ECOG 2 patient")
			}
			return
		}
	}
	// BODH-002 may or may not appear depending on computed score; no failure if absent.
}

// ---- BODH-003: DM2 cardiovascular ----

func TestTrialScreener_DiabetesPatientEligibleForBODH003(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-dm2",
		Diagnosis: "Type 2 DM with hypertension",
		Age:       55,
		ECOGScore: 0,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-003" {
			if trial.MatchScore != 1.0 {
				t.Errorf("BODH-003 match_score=%.2f, want 1.0", trial.MatchScore)
			}
			return
		}
	}
	t.Error("BODH-003 not found in eligible trials for DM2 patient aged 55 with no MACE")
}

func TestTrialScreener_DiabetesPatientWithMIExcludedFromBODH003(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID:  "pt-dm2-mace",
		Diagnosis:  "T2DM",
		Age:        62,
		ECOGScore:  1,
		Exclusions: []string{"myocardial infarction 2019"},
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-003" && trial.MatchScore == 1.0 {
			t.Error("BODH-003 should not score 1.0 for patient with prior MI in exclusions")
		}
	}
}

// ---- BODH-004: solid tumor ----

func TestTrialScreener_SolidTumorEligibleForBODH004(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID:       "pt-solid",
		Diagnosis:       "metastatic colorectal adenocarcinoma",
		Age:             48,
		ECOGScore:       1,
		PriorTreatments: []string{"FOLFOX", "bevacizumab"},
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-004" {
			if trial.MatchScore != 1.0 {
				t.Errorf("BODH-004 match_score=%.2f, want 1.0", trial.MatchScore)
			}
			return
		}
	}
	t.Error("BODH-004 not found for solid tumor patient")
}

// ---- BODH-005: registry (broadest) ----

func TestTrialScreener_AnyAdultEligibleForBODH005(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-any",
		Diagnosis: "essential hypertension",
		Age:       30,
		ECOGScore: 0,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-005" {
			if trial.MatchScore != 1.0 {
				t.Errorf("BODH-005 match_score=%.2f, want 1.0", trial.MatchScore)
			}
			return
		}
	}
	t.Error("BODH-005 registry not in eligible trials for adult patient with a diagnosis")
}

func TestTrialScreener_MinorExcludedFromBODH005(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-minor",
		Diagnosis: "asthma",
		Age:       16,
		ECOGScore: 0,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.TrialID == "BODH-005" && trial.MatchScore == 1.0 {
			t.Error("BODH-005 should not score 1.0 for a minor (age 16)")
		}
	}
}

// ---- match score threshold ----

func TestTrialScreener_LowScoreTrialsExcluded(t *testing.T) {
	// A patient with no matching diagnosis should score 0.0 on BODH-001/002/003/004;
	// only BODH-005 (broadest) may appear.
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-unrelated",
		Diagnosis: "contact dermatitis",
		Age:       25,
		ECOGScore: 0,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for _, trial := range result.EligibleTrials {
		if trial.MatchScore < 0.5 {
			t.Errorf("trial %s appeared with match_score=%.2f (below 0.5 threshold)",
				trial.TrialID, trial.MatchScore)
		}
	}
}

// ---- sort order ----

func TestTrialScreener_ResultsSortedByMatchScoreDescending(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-sort",
		Diagnosis: "NSCLC",
		Age:       55,
		ECOGScore: 1,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)

	for i := 1; i < len(result.EligibleTrials); i++ {
		prev := result.EligibleTrials[i-1].MatchScore
		cur := result.EligibleTrials[i].MatchScore
		if prev < cur {
			t.Errorf("results not sorted descending: index %d score %.2f < index %d score %.2f",
				i-1, prev, i, cur)
		}
	}
}

// ---- total_screened ----

func TestTrialScreener_TotalScreenedEqualsTrialDBSize(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	req := TrialScreenRequest{
		PatientID: "pt-count",
		Diagnosis: "anything",
		Age:       40,
		ECOGScore: 0,
	}
	msgs, err := a.HandleMessage(context.Background(), makeScreenRequest(t, req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	result := decodeTrialResult(t, msgs)
	if result.TotalScreened != len(mockTrials) {
		t.Errorf("TotalScreened=%d, want %d (all mock trials)", result.TotalScreened, len(mockTrials))
	}
}

// ---- error handling ----

func TestTrialScreener_MalformedContentReturnsError(t *testing.T) {
	a := &ClinicalTrialScreenerAgent{}
	msg := agent.NewMessage("test", "trial_screener", agent.RoleUser,
		"trial_screen_request", "not-valid-json", nil)
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Fatal("expected error on malformed JSON content, got nil")
	}
}
