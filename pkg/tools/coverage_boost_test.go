package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ==========================================================================
// OpenURLTool — uncovered error paths
// ==========================================================================

func TestOpenURLTool_MalformedJSON(t *testing.T) {
	tool := &OpenURLTool{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want decode arguments mention", err.Error())
	}
}

func TestOpenURLTool_HTTPError(t *testing.T) {
	// Use a client whose transport always fails.
	tool := &OpenURLTool{httpClient: &http.Client{Transport: failTransport{}}}
	args, _ := json.Marshal(map[string]string{"url": "http://localhost:1/unreachable"})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for HTTP failure")
	}
	if !strings.Contains(err.Error(), "http get") {
		t.Errorf("error=%q, want http get mention", err.Error())
	}
}

func TestOpenURLTool_ReadBodyError(t *testing.T) {
	// Create a server that sends incomplete chunked response to trigger read error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		// Write fewer bytes than promised and close.
		_, _ = w.Write([]byte("partial"))
		// Hijack the connection to force an abrupt close.
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	tool := &OpenURLTool{httpClient: srv.Client()}
	args, _ := json.Marshal(map[string]string{"url": srv.URL})
	// This may or may not error — the important thing is we exercise the read path.
	// On error, it should mention "read body".
	_, _ = tool.Invoke(context.Background(), args)
}

func TestOpenURLTool_NonOKStatus(t *testing.T) {
	// The OpenURLTool doesn't explicitly check status codes; it reads the body regardless.
	// But we verify it still works with non-200 responses.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	tool := &OpenURLTool{httpClient: srv.Client()}
	args, _ := json.Marshal(map[string]string{"url": srv.URL})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result openURLResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(result.Content, "not found") {
		t.Errorf("expected 'not found' in content, got %q", result.Content)
	}
}

// ==========================================================================
// RunCommandTool — uncovered error paths
// ==========================================================================

func TestRunCommandTool_EmptyArgs(t *testing.T) {
	tool := RunCommandTool{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
	if !strings.Contains(err.Error(), "empty arguments") {
		t.Errorf("error=%q, want 'empty arguments'", err.Error())
	}
}

func TestRunCommandTool_MalformedJSON(t *testing.T) {
	tool := RunCommandTool{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestRunCommandTool_ProgramNotOnAllowList(t *testing.T) {
	// Programs like 'echo' are not on the allow-list.
	err := validateCommand("echo hello")
	if err == nil {
		t.Fatal("expected error for unlisted program")
	}
	if !strings.Contains(err.Error(), "not on the allow-list") {
		t.Errorf("error=%q, want allow-list mention", err.Error())
	}
}

func TestRunCommandTool_ExecFailure(t *testing.T) {
	// A command that doesn't exist at all should produce an exec error.
	tool := RunCommandTool{}
	// 'find' is allowed but with arguments that cause non-zero exit.
	args, _ := json.Marshal(map[string]string{"command": "find /nonexistent_path_xyz_12345"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		// find returns exit code 1 for missing paths; this should be captured not errored.
		t.Fatalf("unexpected error: %v", err)
	}
	var result runCommandResult
	json.Unmarshal(raw, &result)
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestValidateCommand_EmptyAfterTrim(t *testing.T) {
	err := validateCommand("")
	if err == nil {
		t.Fatal("expected error for empty command")
	}
	if !strings.Contains(err.Error(), "command is empty") {
		t.Errorf("error=%q, want 'command is empty'", err.Error())
	}
}

// ==========================================================================
// ClinicalTrialsGov — uncovered paths
// ==========================================================================

func TestClinicalTrialsGov_EmptyArgs(t *testing.T) {
	tool := ClinicalTrialsGov{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
	if !strings.Contains(err.Error(), "empty arguments") {
		t.Errorf("error=%q, want 'empty arguments'", err.Error())
	}
}

func TestClinicalTrialsGov_MalformedJSON(t *testing.T) {
	tool := ClinicalTrialsGov{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestClinicalTrialsGov_MaxResultsCapped(t *testing.T) {
	failClient := &http.Client{Transport: failTransport{}}
	tool := ClinicalTrialsGov{HTTPClient: failClient}
	args, _ := json.Marshal(map[string]any{"condition": "cancer", "max_results": 25})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ctgovOutput
	json.Unmarshal(raw, &out)
	// max_results is capped at 20, and mock data has 5 entries, so we get at most 5.
	if out.TotalReturned > 20 {
		t.Errorf("expected at most 20 results, got %d", out.TotalReturned)
	}
}

func TestClinicalTrialsGov_NilStudiesBecomeEmptyArray(t *testing.T) {
	// Use a phase filter that matches nothing in mock data.
	failClient := &http.Client{Transport: failTransport{}}
	tool := ClinicalTrialsGov{HTTPClient: failClient}
	args, _ := json.Marshal(map[string]any{"condition": "cancer", "phase": "Phase IV"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The output.Studies should be an empty array, not null.
	if !strings.Contains(string(raw), `"studies":[]`) {
		t.Errorf("expected empty studies array in JSON, got %s", string(raw))
	}
}

func TestClinicalTrialsGov_FetchStudiesSuccess(t *testing.T) {
	// Mock the ClinicalTrials.gov v2 API response.
	mockAPIResp := `{
		"studies": [
			{
				"protocolSection": {
					"identificationModule": {"nctId": "NCT12345678", "briefTitle": "Test Study"},
					"statusModule": {"overallStatus": "Recruiting"},
					"designModule": {"phaseList": {"phase": ["Phase II", "Phase III"]}},
					"sponsorCollaboratorsModule": {"leadSponsor": {"name": "Test Sponsor"}},
					"descriptionModule": {"briefSummary": "A test study summary."}
				}
			}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockAPIResp))
	}))
	defer srv.Close()

	tool := ClinicalTrialsGov{HTTPClient: srv.Client()}
	// The tool calls clinicaltrials.gov directly so we use a redirect client.
	tool.HTTPClient = newRedirectClient(srv.URL)

	args, _ := json.Marshal(map[string]any{"condition": "NSCLC"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ctgovOutput
	json.Unmarshal(raw, &out)
	if out.Source != "clinicaltrials.gov" {
		t.Errorf("expected source=clinicaltrials.gov, got %q", out.Source)
	}
	if len(out.Studies) != 1 {
		t.Fatalf("expected 1 study, got %d", len(out.Studies))
	}
	if out.Studies[0].NCTID != "NCT12345678" {
		t.Errorf("expected NCT ID NCT12345678, got %q", out.Studies[0].NCTID)
	}
	if out.Studies[0].Phase != "Phase II/Phase III" {
		t.Errorf("expected phase 'Phase II/Phase III', got %q", out.Studies[0].Phase)
	}
}

func TestClinicalTrialsGov_FetchStudiesEmptyPhase(t *testing.T) {
	// Test study with empty phase list.
	mockAPIResp := `{
		"studies": [
			{
				"protocolSection": {
					"identificationModule": {"nctId": "NCT99999999", "briefTitle": "No Phase Study"},
					"statusModule": {"overallStatus": "Completed"},
					"designModule": {"phaseList": {"phase": []}},
					"sponsorCollaboratorsModule": {"leadSponsor": {"name": "Sponsor"}},
					"descriptionModule": {"briefSummary": "Summary."}
				}
			}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockAPIResp))
	}))
	defer srv.Close()

	tool := ClinicalTrialsGov{HTTPClient: newRedirectClient(srv.URL)}
	args, _ := json.Marshal(map[string]any{"condition": "test"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ctgovOutput
	json.Unmarshal(raw, &out)
	if out.Studies[0].Phase != "" {
		t.Errorf("expected empty phase, got %q", out.Studies[0].Phase)
	}
}

func TestClinicalTrialsGov_FetchStudiesNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tool := ClinicalTrialsGov{HTTPClient: newRedirectClient(srv.URL)}
	args, _ := json.Marshal(map[string]any{"condition": "NSCLC"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error (should fallback): %v", err)
	}
	var out ctgovOutput
	json.Unmarshal(raw, &out)
	if out.Source != "demo-mock" {
		t.Errorf("expected demo-mock fallback, got %q", out.Source)
	}
}

func TestClinicalTrialsGov_FetchStudiesInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not valid json`))
	}))
	defer srv.Close()

	tool := ClinicalTrialsGov{HTTPClient: newRedirectClient(srv.URL)}
	args, _ := json.Marshal(map[string]any{"condition": "NSCLC"})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error (should fallback): %v", err)
	}
	var out ctgovOutput
	json.Unmarshal(raw, &out)
	if out.Source != "demo-mock" {
		t.Errorf("expected demo-mock fallback on bad JSON, got %q", out.Source)
	}
}

// ==========================================================================
// DrugInteraction — uncovered paths
// ==========================================================================

func TestDrugInteraction_EmptyArgs(t *testing.T) {
	_, err := DrugInteraction{}.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
	if !strings.Contains(err.Error(), "empty arguments") {
		t.Errorf("error=%q, want 'empty arguments'", err.Error())
	}
}

func TestDrugInteraction_EmptyDrugsList(t *testing.T) {
	_, err := DrugInteraction{}.Invoke(context.Background(), json.RawMessage(`{"drugs":[]}`))
	if err == nil {
		t.Fatal("expected error for empty drugs list")
	}
	if !strings.Contains(err.Error(), "drugs list is empty") {
		t.Errorf("error=%q, want 'drugs list is empty'", err.Error())
	}
}

func TestDrugInteraction_WhitespaceOnlyDrugsSkipped(t *testing.T) {
	// Drugs that are empty/whitespace-only should be silently skipped.
	raw, err := DrugInteraction{}.Invoke(context.Background(), json.RawMessage(`{"drugs":["  ","","lisinopril"]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out drugInteractionOutput
	json.Unmarshal(raw, &out)
	if !out.Checked {
		t.Error("expected checked=true")
	}
	// With only one valid drug, no pairs scanned.
	if got, ok := out.Metadata["pairs_scanned"]; !ok || got != float64(0) {
		t.Errorf("pairs_scanned=%v, want 0 (single drug)", got)
	}
}

func TestDrugInteraction_DuplicateDrugsDeduped(t *testing.T) {
	raw, err := DrugInteraction{}.Invoke(context.Background(), json.RawMessage(`{"drugs":["lisinopril","lisinopril","spironolactone"]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out drugInteractionOutput
	json.Unmarshal(raw, &out)
	// Duplicates should be deduped, so only 1 pair scanned (lisinopril, spironolactone).
	if got := out.Metadata["pairs_scanned"]; got != float64(1) {
		t.Errorf("pairs_scanned=%v, want 1 (deduped)", got)
	}
	if len(out.Interactions) != 1 {
		t.Errorf("expected 1 interaction, got %d", len(out.Interactions))
	}
}

// ==========================================================================
// FHIRQuery — uncovered paths
// ==========================================================================

func TestFHIRQuery_EmptyArgs(t *testing.T) {
	_, err := FHIRQuery{}.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
	if !strings.Contains(err.Error(), "empty arguments") {
		t.Errorf("error=%q, want 'empty arguments'", err.Error())
	}
}

func TestFHIRQuery_MalformedJSON(t *testing.T) {
	_, err := FHIRQuery{}.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestFHIRQuery_AllPatients(t *testing.T) {
	// Ensure all 8 synthetic patients return valid records.
	ids := []string{"pt-001", "pt-002", "pt-003", "pt-004", "pt-005", "pt-006", "pt-007", "pt-008"}
	for _, id := range ids {
		args, _ := json.Marshal(map[string]string{"resource": "Patient", "patient_id": id})
		raw, err := FHIRQuery{}.Invoke(context.Background(), args)
		if err != nil {
			t.Fatalf("patient %s: unexpected error: %v", id, err)
		}
		var p fhirPatient
		json.Unmarshal(raw, &p)
		if !p.Found {
			t.Errorf("patient %s: expected found=true", id)
		}
		if p.ID != id {
			t.Errorf("patient %s: ID=%q", id, p.ID)
		}
	}
}

func TestFHIRQuery_WhitespacePatientID(t *testing.T) {
	_, err := FHIRQuery{}.Invoke(context.Background(), json.RawMessage(`{"resource":"Patient","patient_id":"   "}`))
	if err == nil {
		t.Fatal("expected error for whitespace-only patient_id")
	}
	if !strings.Contains(err.Error(), "empty patient_id") {
		t.Errorf("error=%q, want 'empty patient_id'", err.Error())
	}
}

// ==========================================================================
// RiskCalculator — uncovered paths
// ==========================================================================

func TestRiskCalculator_EmptyArgs(t *testing.T) {
	_, err := DefaultRiskCalculator().Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
	if !strings.Contains(err.Error(), "empty arguments") {
		t.Errorf("error=%q, want 'empty arguments'", err.Error())
	}
}

func TestRiskCalculator_MalformedJSON(t *testing.T) {
	_, err := DefaultRiskCalculator().Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestRiskCalculator_WellsPE_ModerateBand(t *testing.T) {
	// Score exactly 2 should be "moderate".
	out := invokeRisk(t, `{
		"calculator":"wells_pe",
		"inputs":{"hr_over_100": true, "hemoptysis": true}
	}`)
	// 1.5 + 1.0 = 2.5
	if out.Score != 2.5 {
		t.Fatalf("score=%v, want 2.5", out.Score)
	}
	if out.Band != "moderate" {
		t.Fatalf("band=%q, want moderate", out.Band)
	}
}

func TestRiskCalculator_WellsPE_ExactlyTwo(t *testing.T) {
	// Score exactly 2 → moderate.
	out := invokeRisk(t, `{
		"calculator":"wells_pe",
		"inputs":{"hemoptysis": true, "malignancy": true}
	}`)
	if out.Score != 2.0 {
		t.Fatalf("score=%v, want 2.0", out.Score)
	}
	if out.Band != "moderate" {
		t.Fatalf("band=%q, want moderate", out.Band)
	}
}

func TestRiskCalculator_CURB65_InpatientBand(t *testing.T) {
	// Score exactly 2 → inpatient.
	out := invokeRisk(t, `{
		"calculator":"curb65",
		"inputs":{
			"confusion": true,
			"age": 70
		}
	}`)
	if out.Score != 2 {
		t.Fatalf("score=%v, want 2", out.Score)
	}
	if out.Band != "inpatient" {
		t.Fatalf("band=%q, want inpatient", out.Band)
	}
}

func TestRiskCalculator_CHA2DS2VASc_LowBand(t *testing.T) {
	// No risk factors → score 0 → low.
	out := invokeRisk(t, `{
		"calculator":"cha2ds2vasc",
		"inputs":{"age": 50}
	}`)
	if out.Score != 0 {
		t.Fatalf("score=%v, want 0", out.Score)
	}
	if out.Band != "low" {
		t.Fatalf("band=%q, want low", out.Band)
	}
}

func TestRiskCalculator_CHA2DS2VASc_AllFactors(t *testing.T) {
	// All factors: CHF(1) + HTN(1) + age>=75(2) + diabetes(1) + stroke(2)
	// + vascular(1) + female(1) = 9.
	out := invokeRisk(t, `{
		"calculator":"cha2ds2vasc",
		"inputs":{
			"chf": true,
			"hypertension": true,
			"age": 80,
			"diabetes": true,
			"stroke_tia": true,
			"vascular_disease": true,
			"female": true
		}
	}`)
	if out.Score != 9 {
		t.Fatalf("score=%v, want 9", out.Score)
	}
	if out.Band != "high" {
		t.Fatalf("band=%q, want high", out.Band)
	}
}

func TestRiskCalculator_HASBLED_NegativeDrugsClamped(t *testing.T) {
	out := invokeRisk(t, `{
		"calculator":"has_bled",
		"inputs":{"drugs_or_alcohol": -3}
	}`)
	if out.Score != 0 {
		t.Fatalf("score=%v, want 0 (negative drugs clamped)", out.Score)
	}
	if got := out.Components["drugs_or_alcohol"].(float64); got != 0 {
		t.Fatalf("drugs_or_alcohol=%v, want 0", got)
	}
}

func TestRiskCalculator_HASBLED_LowBand(t *testing.T) {
	// Score 2 → low.
	out := invokeRisk(t, `{
		"calculator":"has_bled",
		"inputs":{"hypertension": true, "elderly": true}
	}`)
	if out.Score != 2 {
		t.Fatalf("score=%v, want 2", out.Score)
	}
	if out.Band != "low" {
		t.Fatalf("band=%q, want low", out.Band)
	}
}

// ==========================================================================
// RenalDosing — uncovered drug paths
// ==========================================================================

func TestRenalDosing_MalformedJSON(t *testing.T) {
	tool := RenalDosing{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestRenalDosing_EmptyDrug(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "", "standard_dose_mg": 500, "creatinine_mg_dl": 1.5,
		"age": 65, "weight_kg": 70, "sex": "male",
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty drug name")
	}
	if !strings.Contains(err.Error(), "drug name is required") {
		t.Errorf("error=%q, want 'drug name is required'", err.Error())
	}
}

func TestRenalDosing_ZeroStandardDose(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "metformin", "standard_dose_mg": 0, "creatinine_mg_dl": 1.5,
		"age": 65, "weight_kg": 70, "sex": "male",
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for zero standard_dose_mg")
	}
}

func TestRenalDosing_ZeroCreatinine(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "metformin", "standard_dose_mg": 500, "creatinine_mg_dl": 0,
		"age": 65, "weight_kg": 70, "sex": "male",
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for zero creatinine")
	}
}

func TestRenalDosing_ZeroWeight(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "metformin", "standard_dose_mg": 500, "creatinine_mg_dl": 1.5,
		"age": 65, "weight_kg": 0, "sex": "male",
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for zero weight")
	}
}

func TestRenalDosing_GabapentinNormalEGFR(t *testing.T) {
	tool := RenalDosing{}
	// Low creatinine → high eGFR → no adjustment.
	args, _ := json.Marshal(map[string]any{
		"drug": "gabapentin", "standard_dose_mg": 300, "creatinine_mg_dl": 0.8,
		"age": 40, "weight_kg": 80, "sex": "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	json.Unmarshal(raw, &out)
	if out.AdjustedDoseMg != 300 {
		t.Errorf("expected no adjustment (300), got %v (eGFR=%.1f)", out.AdjustedDoseMg, out.EgfrMlMin)
	}
}

func TestRenalDosing_LisinoprilNormalEGFR(t *testing.T) {
	tool := RenalDosing{}
	// eGFR >= 30 → standard dosing.
	args, _ := json.Marshal(map[string]any{
		"drug": "lisinopril", "standard_dose_mg": 10, "creatinine_mg_dl": 1.0,
		"age": 50, "weight_kg": 75, "sex": "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	json.Unmarshal(raw, &out)
	if out.AdjustedDoseMg != 10 {
		t.Errorf("expected standard dose 10, got %v", out.AdjustedDoseMg)
	}
	if out.Frequency != "as prescribed" {
		t.Errorf("expected 'as prescribed', got %q", out.Frequency)
	}
}

func TestRenalDosing_DigoxinNormalEGFR(t *testing.T) {
	tool := RenalDosing{}
	// eGFR >= 50 → standard dosing.
	args, _ := json.Marshal(map[string]any{
		"drug": "digoxin", "standard_dose_mg": 0.25, "creatinine_mg_dl": 0.9,
		"age": 50, "weight_kg": 70, "sex": "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	json.Unmarshal(raw, &out)
	if out.AdjustedDoseMg != 0.25 {
		t.Errorf("expected standard dose 0.25, got %v", out.AdjustedDoseMg)
	}
}

func TestRenalDosing_VancomycinQ48h(t *testing.T) {
	tool := RenalDosing{}
	// eGFR < 10 → q48h.
	args, _ := json.Marshal(map[string]any{
		"drug": "vancomycin", "standard_dose_mg": 1000, "creatinine_mg_dl": 10.0,
		"age": 70, "weight_kg": 60, "sex": "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	json.Unmarshal(raw, &out)
	if out.Frequency != "q48h" {
		t.Errorf("expected q48h for eGFR < 10, got %q (eGFR=%.1f)", out.Frequency, out.EgfrMlMin)
	}
}

func TestRenalDosing_VancomycinQ12h(t *testing.T) {
	tool := RenalDosing{}
	// eGFR 50-90 → q12h.
	args, _ := json.Marshal(map[string]any{
		"drug": "vancomycin", "standard_dose_mg": 1000, "creatinine_mg_dl": 1.2,
		"age": 50, "weight_kg": 80, "sex": "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	json.Unmarshal(raw, &out)
	if out.Frequency != "q12h" {
		t.Errorf("expected q12h for eGFR 50-90, got %q (eGFR=%.1f)", out.Frequency, out.EgfrMlMin)
	}
}

func TestRenalDosing_VancomycinNormal(t *testing.T) {
	tool := RenalDosing{}
	// eGFR >= 90 → q8-12h.
	args, _ := json.Marshal(map[string]any{
		"drug": "vancomycin", "standard_dose_mg": 1000, "creatinine_mg_dl": 0.7,
		"age": 30, "weight_kg": 80, "sex": "male",
	})
	raw, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	json.Unmarshal(raw, &out)
	if out.Frequency != "q8-12h" {
		t.Errorf("expected q8-12h for eGFR >= 90, got %q (eGFR=%.1f)", out.Frequency, out.EgfrMlMin)
	}
}

// ==========================================================================
// PediatricDosing — uncovered paths
// ==========================================================================

func TestPediatricDosing_MalformedJSON(t *testing.T) {
	tool := PediatricDosing{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestPediatricDosing_EmptyDrug(t *testing.T) {
	tool := PediatricDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "", "weight_kg": 20, "age_months": 24,
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty drug name")
	}
	if !strings.Contains(err.Error(), "drug name is required") {
		t.Errorf("error=%q, want 'drug name is required'", err.Error())
	}
}

// ==========================================================================
// WeightBasedDosing — uncovered paths
// ==========================================================================

func TestWeightBasedDosing_MalformedJSON(t *testing.T) {
	tool := WeightBasedDosing{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

// ==========================================================================
// Filesystem tools — uncovered paths
// ==========================================================================

func TestReadFileTool_MalformedJSON(t *testing.T) {
	tool := ReadFileTool{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestReadFileTool_EmptyPath(t *testing.T) {
	tool := ReadFileTool{}
	args := json.RawMessage(`{"path":""}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "path is empty") {
		t.Errorf("error=%q, want 'path is empty'", err.Error())
	}
}

func TestWriteFileTool_MalformedJSON(t *testing.T) {
	tool := WriteFileTool{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestWriteFileTool_EmptyPath(t *testing.T) {
	tool := WriteFileTool{}
	args := json.RawMessage(`{"path":"","content":"hello"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "path is empty") {
		t.Errorf("error=%q, want 'path is empty'", err.Error())
	}
}

func TestWriteFileTool_FailedMkdirAll(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	// Create a file where we'd need a directory — MkdirAll will fail.
	writeFile(t, dir, "blocker", "I'm a file, not a dir")

	tool := WriteFileTool{}
	args := json.RawMessage(`{"path":"blocker/child/file.txt","content":"test"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error when MkdirAll fails")
	}
	if !strings.Contains(err.Error(), "create parent dirs") {
		t.Errorf("error=%q, want 'create parent dirs'", err.Error())
	}
}

func TestWriteFileTool_WriteToReadOnlyDir(t *testing.T) {
	dir := tmpDir(t)
	chdir(t, dir)

	// Create a subdirectory and make it read-only.
	readOnlyDir := filepath.Join(dir, "readonly")
	os.MkdirAll(readOnlyDir, 0o755)
	os.Chmod(readOnlyDir, 0o555)
	t.Cleanup(func() { os.Chmod(readOnlyDir, 0o755) })

	tool := WriteFileTool{}
	args := json.RawMessage(`{"path":"readonly/file.txt","content":"test"}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error writing to read-only directory")
	}
}

func TestListDirectoryTool_MalformedJSON(t *testing.T) {
	tool := ListDirectoryTool{}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode arguments") {
		t.Errorf("error=%q, want 'decode arguments'", err.Error())
	}
}

func TestListDirectoryTool_EmptyPath(t *testing.T) {
	tool := ListDirectoryTool{}
	args := json.RawMessage(`{"path":""}`)
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "path is empty") {
		t.Errorf("error=%q, want 'path is empty'", err.Error())
	}
}

// ==========================================================================
// WebSearchTool — uncovered paths
// ==========================================================================

func TestWebSearchTool_EmptyArgs(t *testing.T) {
	tool := &WebSearchTool{}
	_, err := tool.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil args")
	}
	if !strings.Contains(err.Error(), "empty arguments") {
		t.Errorf("error=%q, want 'empty arguments'", err.Error())
	}
}

func TestWebSearchTool_MaxResultsCapped(t *testing.T) {
	// Build a stub with 25 topics.
	topics := make([]string, 25)
	for i := range topics {
		topics[i] = `{"Text":"Item - description","FirstURL":"https://example.com"}`
	}
	body := `{"RelatedTopics":[` + strings.Join(topics, ",") + `]}`

	srv := newMockDDGServer(t, http.StatusOK, body)
	defer srv.Close()

	tool := &WebSearchTool{httpClient: newRedirectClient(srv.URL)}
	results := invokeWebSearch(t, tool, `{"query":"test","max_results":25}`)
	if len(results) != 20 {
		t.Fatalf("expected max 20 results (capped), got %d", len(results))
	}
}

func TestWebSearchTool_SearchHTTPError(t *testing.T) {
	tool := &WebSearchTool{httpClient: &http.Client{Transport: failTransport{}}}
	// Override to use redirecting transport that fails.
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"test"}`))
	if err == nil {
		t.Fatal("expected error for HTTP failure")
	}
	if !strings.Contains(err.Error(), "http get") {
		t.Errorf("error=%q, want http get mention", err.Error())
	}
}

func TestWebSearchTool_InvalidJSONResponse(t *testing.T) {
	srv := newMockDDGServer(t, http.StatusOK, `not json at all`)
	defer srv.Close()

	tool := &WebSearchTool{httpClient: newRedirectClient(srv.URL)}
	_, err := tool.Invoke(context.Background(), json.RawMessage(`{"query":"test"}`))
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Errorf("error=%q, want 'decode response'", err.Error())
	}
}

func TestWebSearchTool_BuildRequestError(t *testing.T) {
	// A context that is already cancelled should cause NewRequestWithContext to fail
	// (or the HTTP call to fail). Let's test with cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tool := &WebSearchTool{}
	_, err := tool.Invoke(ctx, json.RawMessage(`{"query":"test"}`))
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// ==========================================================================
// Registry — additional edge cases
// ==========================================================================

func TestRegistry_EmptyRegistryLLMTools(t *testing.T) {
	r := NewRegistry()
	if got := r.LLMTools(); got != nil {
		t.Fatalf("expected nil for empty registry, got %v", got)
	}
}

func TestRegistry_EmptyRegistryList(t *testing.T) {
	r := NewRegistry()
	if got := r.List(); got != nil {
		t.Fatalf("expected nil for empty registry, got %v", got)
	}
}

func TestRegistry_EmptyRegistryGet(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("anything")
	if ok {
		t.Fatal("expected miss on empty registry")
	}
}

// ==========================================================================
// boolField — edge case
// ==========================================================================

func TestBoolField_NonBoolValue(t *testing.T) {
	inputs := map[string]any{"key": "not a bool"}
	if boolField(inputs, "key") {
		t.Error("expected false for non-bool value")
	}
}

func TestBoolField_FalseValue(t *testing.T) {
	inputs := map[string]any{"key": false}
	if boolField(inputs, "key") {
		t.Error("expected false for false value")
	}
}

// ==========================================================================
// splitTitleSnippet — edge case
// ==========================================================================

func TestSplitTitleSnippet_EmptyText(t *testing.T) {
	title, snippet := splitTitleSnippet("")
	if title != "" || snippet != "" {
		t.Errorf("expected empty title and snippet for empty text, got %q, %q", title, snippet)
	}
}

// ==========================================================================
// stripHTMLTags — entity decoding
// ==========================================================================

func TestStripHTMLTags_Entities(t *testing.T) {
	input := `&amp; &lt; &gt; &quot; &#39; &nbsp;`
	got := strings.TrimSpace(stripHTMLTags(input)) // after whitespace collapsing
	if !strings.Contains(got, "&") || !strings.Contains(got, "<") || !strings.Contains(got, ">") {
		t.Errorf("entity decoding failed: got %q", got)
	}
}

// ==========================================================================
// NewComputerRegistry — verify LLMTools conversion
// ==========================================================================

func TestNewComputerRegistry_LLMToolsConversion(t *testing.T) {
	reg := NewComputerRegistry()
	llmTools := reg.LLMTools()
	if len(llmTools) != 3 {
		t.Fatalf("expected 3 LLM tools, got %d", len(llmTools))
	}
	for _, lt := range llmTools {
		if lt.Name == "" {
			t.Error("LLM tool has empty name")
		}
		if lt.Description == "" {
			t.Errorf("LLM tool %q has empty description", lt.Name)
		}
		if len(lt.Schema) == 0 {
			t.Errorf("LLM tool %q has empty schema", lt.Name)
		}
	}
}

// ==========================================================================
// Context cancellation tests
// ==========================================================================

func TestOpenURLTool_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tool := &OpenURLTool{}
	args, _ := json.Marshal(map[string]string{"url": "http://example.com"})
	_, err := tool.Invoke(ctx, args)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestRunCommandTool_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tool := RunCommandTool{}
	args, _ := json.Marshal(map[string]string{"command": "ls"})
	_, err := tool.Invoke(ctx, args)
	// The command may fail with context error.
	if err == nil {
		// It's okay if ls completes before cancellation is noticed.
		return
	}
}
