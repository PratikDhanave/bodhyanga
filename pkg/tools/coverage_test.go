package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Computer tools: Description, Schema coverage
// ---------------------------------------------------------------------------

func TestOpenURLTool_DescriptionAndSchema(t *testing.T) {
	tool := OpenURLTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
	if len(tool.Schema()) == 0 {
		t.Error("expected non-empty schema")
	}
}

func TestRunCommandTool_DescriptionAndSchema(t *testing.T) {
	tool := RunCommandTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
	if len(tool.Schema()) == 0 {
		t.Error("expected non-empty schema")
	}
}

func TestTakeScreenshotTool_DescriptionSchemaAndInvoke(t *testing.T) {
	tool := TakeScreenshotTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
	if len(tool.Schema()) == 0 {
		t.Error("expected non-empty schema")
	}
	result, err := tool.Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out screenshotResult
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.Available {
		t.Error("expected available=false")
	}
}

// ---------------------------------------------------------------------------
// OpenURLTool: client() method
// ---------------------------------------------------------------------------

func TestOpenURLTool_ClientInjection(t *testing.T) {
	custom := &http.Client{}
	tool := &OpenURLTool{httpClient: custom}
	if tool.client() != custom {
		t.Error("expected custom client")
	}
}

func TestOpenURLTool_ClientDefault(t *testing.T) {
	tool := &OpenURLTool{}
	if tool.client() == nil {
		t.Error("expected non-nil default client")
	}
}

// ---------------------------------------------------------------------------
// truncateToValidUTF8
// ---------------------------------------------------------------------------

func TestTruncateToValidUTF8_ValidInput(t *testing.T) {
	input := []byte("hello world")
	out := truncateToValidUTF8(input)
	if string(out) != "hello world" {
		t.Errorf("expected unchanged, got %q", string(out))
	}
}

func TestTruncateToValidUTF8_InvalidTrailingBytes(t *testing.T) {
	input := []byte("hello\xc3")
	out := truncateToValidUTF8(input)
	if string(out) != "hello" {
		t.Errorf("expected hello, got %q", string(out))
	}
}

func TestTruncateToValidUTF8_Empty(t *testing.T) {
	out := truncateToValidUTF8(nil)
	if len(out) != 0 {
		t.Error("expected empty")
	}
}

// ---------------------------------------------------------------------------
// validateCommand: additional edge cases
// ---------------------------------------------------------------------------

func TestValidateCommand_PathPrefixStripping(t *testing.T) {
	err := validateCommand("/usr/bin/ls -la")
	if err != nil {
		t.Errorf("expected allowed with path prefix, got %v", err)
	}
}

func TestValidateCommand_GitWithoutSubcommand(t *testing.T) {
	err := validateCommand("git")
	if err == nil {
		t.Error("expected error for git without subcommand")
	}
}

func TestValidateCommand_GitDisallowedSub(t *testing.T) {
	err := validateCommand("git push")
	if err == nil {
		t.Error("expected error for disallowed git subcommand")
	}
}

func TestValidateCommand_EmptyAfterSplit(t *testing.T) {
	err := validateCommand("   ")
	if err == nil {
		t.Error("expected error for empty command")
	}
}

// ---------------------------------------------------------------------------
// RunCommandTool: Invoke with exit-code > 0, output truncation
// ---------------------------------------------------------------------------

func TestRunCommandTool_NonZeroExit(t *testing.T) {
	tool := RunCommandTool{}
	args, _ := json.Marshal(map[string]string{"command": "ls /nonexistent_dir_xyz"})
	result, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out runCommandResult
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if out.ExitCode == 0 {
		t.Error("expected non-zero exit code for nonexistent directory")
	}
}

// ---------------------------------------------------------------------------
// WebSearchTool: client(), empty results, extractResults sub-groups
// ---------------------------------------------------------------------------

func TestWebSearchTool_ClientInjection(t *testing.T) {
	custom := &http.Client{}
	tool := &WebSearchTool{httpClient: custom}
	if tool.client() != custom {
		t.Error("expected custom client")
	}
}

func TestWebSearchTool_ClientDefault(t *testing.T) {
	tool := &WebSearchTool{}
	if tool.client() == nil {
		t.Error("expected non-nil default client")
	}
}

func TestWebSearchTool_EmptyResults(t *testing.T) {
	results := extractResults(nil, 5)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestExtractResults_SubGroups(t *testing.T) {
	topics := []duckduckgoTopic{
		{
			Name: "group1",
			Topics: []duckduckgoTopic{
				{Text: "Title - Snippet", FirstURL: "https://example.com"},
			},
		},
		{Text: "", FirstURL: ""}, // empty URL, should be skipped
		{Text: "Direct - Result", FirstURL: "https://direct.com"},
	}
	results := extractResults(topics, 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

// WebSearchTool's search method uses a hardcoded URL so HTTP error testing
// is covered indirectly through the splitTitleSnippet and extractResults tests.

// ---------------------------------------------------------------------------
// Dosing tools: edge cases
// ---------------------------------------------------------------------------

func TestWeightBasedDosing_ZeroWeight(t *testing.T) {
	tool := WeightBasedDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "amox", "dose_per_kg": 10, "weight_kg": 0,
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for zero weight")
	}
}

func TestWeightBasedDosing_ZeroDosePerKg(t *testing.T) {
	tool := WeightBasedDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "amox", "dose_per_kg": 0, "weight_kg": 10,
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for zero dose_per_kg")
	}
}

func TestRenalDosing_NegativeAge(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "metformin", "standard_dose_mg": 500, "creatinine_mg_dl": 1.5,
		"age": -1, "weight_kg": 70, "sex": "male",
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for negative age")
	}
}

func TestRenalDosing_AgeOver130(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "metformin", "standard_dose_mg": 500, "creatinine_mg_dl": 1.5,
		"age": 131, "weight_kg": 70, "sex": "male",
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for age > 130")
	}
}

func TestRenalDosing_UnknownDrug(t *testing.T) {
	tool := RenalDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "unknowndrug", "standard_dose_mg": 500, "creatinine_mg_dl": 1.5,
		"age": 65, "weight_kg": 70, "sex": "male",
	})
	result, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out renalDosingOutput
	json.Unmarshal(result, &out)
	if out.AdjustedDoseMg != 500 {
		t.Errorf("expected unadjusted dose of 500, got %f", out.AdjustedDoseMg)
	}
}

func TestCockcroftGault_FemaleWithHighAge(t *testing.T) {
	// Test cockcroftGault with age > 140 which gives negative, clamped to 0.
	egfr := cockcroftGault(141, 70, 1.5, "female")
	if egfr != 0 {
		t.Errorf("expected 0 for age>140, got %f", egfr)
	}
}

func TestPediatricDosing_NegativeAge(t *testing.T) {
	tool := PediatricDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "ibuprofen", "weight_kg": 20, "age_months": -1,
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for negative age_months")
	}
}

func TestPediatricDosing_UnknownDrug(t *testing.T) {
	tool := PediatricDosing{}
	args, _ := json.Marshal(map[string]any{
		"drug": "unknowndrug", "weight_kg": 20, "age_months": 48,
	})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for unknown drug")
	}
}

// ---------------------------------------------------------------------------
// RiskCalculator: numField edge cases
// ---------------------------------------------------------------------------

func TestNumField_AllTypes(t *testing.T) {
	inputs := map[string]any{
		"f64":   float64(1.0),
		"f32":   float32(2.0),
		"int":   int(3),
		"int64": int64(4),
		"str":   "not a number",
	}
	if v := numField(inputs, "f64"); v != 1.0 {
		t.Errorf("expected 1.0, got %f", v)
	}
	if v := numField(inputs, "f32"); v != 2.0 {
		t.Errorf("expected 2.0, got %f", v)
	}
	if v := numField(inputs, "int"); v != 3.0 {
		t.Errorf("expected 3.0, got %f", v)
	}
	if v := numField(inputs, "int64"); v != 4.0 {
		t.Errorf("expected 4.0, got %f", v)
	}
	if v := numField(inputs, "str"); v != 0 {
		t.Errorf("expected 0 for string, got %f", v)
	}
	if v := numField(inputs, "missing"); v != 0 {
		t.Errorf("expected 0 for missing, got %f", v)
	}
}

func TestRiskCalculator_EmptyCalculators(t *testing.T) {
	rc := &RiskCalculator{calculators: nil}
	args, _ := json.Marshal(riskInput{Calculator: "wells_pe", Inputs: map[string]any{}})
	_, err := rc.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for nil calculators map")
	}
}

// ---------------------------------------------------------------------------
// ClinicalTrialsGov: Name, Description, Schema
// ---------------------------------------------------------------------------

func TestClinicalTrialsGov_NameDescriptionSchema(t *testing.T) {
	tool := ClinicalTrialsGov{}
	if tool.Name() != "search_clinical_trials" {
		t.Errorf("unexpected name: %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
	if len(tool.Schema()) == 0 {
		t.Error("expected non-empty schema")
	}
}

func TestClinicalTrialsGov_Invoke_Fallback(t *testing.T) {
	// Use an http.Client whose transport always fails to force fallback to mock data.
	failClient := &http.Client{
		Transport: failTransport{},
	}

	tool := ClinicalTrialsGov{HTTPClient: failClient}
	args, _ := json.Marshal(map[string]any{"condition": "NSCLC"})
	result, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out ctgovOutput
	json.Unmarshal(result, &out)
	if out.Source != "demo-mock" {
		t.Errorf("expected demo-mock source, got %q", out.Source)
	}
}

type failTransport struct{}

func (failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("network error")
}

func TestClinicalTrialsGov_Invoke_EmptyCondition(t *testing.T) {
	tool := ClinicalTrialsGov{}
	args, _ := json.Marshal(map[string]any{"condition": ""})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for empty condition")
	}
}

func TestFilterMockStudies_PhaseFilter(t *testing.T) {
	in := ctgovInput{Phase: "Phase III"}
	results := filterMockStudies(in, 10)
	for _, r := range results {
		if r.Phase != "Phase III" {
			t.Errorf("expected Phase III, got %q", r.Phase)
		}
	}
}

// ---------------------------------------------------------------------------
// DrugInteraction: Name, Description, Schema
// ---------------------------------------------------------------------------

func TestDrugInteraction_NameDescriptionSchema(t *testing.T) {
	tool := DrugInteraction{}
	if tool.Name() != "drug_interaction" {
		t.Errorf("unexpected name: %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
	if len(tool.Schema()) == 0 {
		t.Error("expected non-empty schema")
	}
}

// ---------------------------------------------------------------------------
// FHIRQuery: Name, Description, Schema
// ---------------------------------------------------------------------------

func TestFHIRQuery_NameDescriptionSchema(t *testing.T) {
	tool := FHIRQuery{}
	if tool.Name() != "fhir_query" {
		t.Errorf("unexpected name: %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
	if len(tool.Schema()) == 0 {
		t.Error("expected non-empty schema")
	}
}

func TestFHIRQuery_UnsupportedResource(t *testing.T) {
	tool := FHIRQuery{}
	args, _ := json.Marshal(map[string]any{"resource": "Observation", "patient_id": "pt-001"})
	_, err := tool.Invoke(context.Background(), args)
	if err == nil {
		t.Error("expected error for unsupported resource")
	}
}

func TestFHIRQuery_EmptyResource_DefaultsToPatient(t *testing.T) {
	tool := FHIRQuery{}
	args, _ := json.Marshal(map[string]any{"resource": "", "patient_id": "pt-001"})
	result, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out fhirPatient
	json.Unmarshal(result, &out)
	if !out.Found {
		t.Error("expected found for pt-001 with empty resource")
	}
}

// ---------------------------------------------------------------------------
// Filesystem: ReadFile truncation, symlink validation
// ---------------------------------------------------------------------------

func TestReadFile_Truncation(t *testing.T) {
	dir := t.TempDir()
	// Create a file larger than maxReadBytes.
	bigContent := make([]byte, maxReadBytes+1000)
	for i := range bigContent {
		bigContent[i] = 'A'
	}
	p := filepath.Join(dir, "bigfile.txt")
	os.WriteFile(p, bigContent, 0o644)

	// Invoke from within the tmp dir context.
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	tool := ReadFileTool{}
	args, _ := json.Marshal(map[string]string{"path": "bigfile.txt"})
	result, err := tool.Invoke(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out readFileOutput
	json.Unmarshal(result, &out)
	if !out.Truncated {
		t.Error("expected truncated=true")
	}
	if out.Bytes != len(bigContent) {
		t.Errorf("expected total bytes %d, got %d", len(bigContent), out.Bytes)
	}
}

func TestValidatePath_AbsolutePathAfterClean(t *testing.T) {
	// This tests the path that goes through filepath.Clean.
	err := validatePath(".")
	if err != nil {
		t.Errorf("expected allowed for '.', got %v", err)
	}
}

// ---------------------------------------------------------------------------
// RiskCalculator: Name, Description, Schema
// ---------------------------------------------------------------------------

func TestRiskCalculator_NameDescriptionSchema(t *testing.T) {
	rc := DefaultRiskCalculator()
	if rc.Name() != "risk_calculator" {
		t.Errorf("unexpected name: %q", rc.Name())
	}
	if rc.Description() == "" {
		t.Error("expected non-empty description")
	}
	if len(rc.Schema()) == 0 {
		t.Error("expected non-empty schema")
	}
}
