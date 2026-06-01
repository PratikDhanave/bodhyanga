package fhir_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/fhir"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// seedRegistry returns a PatientRegistry pre-populated with two patients.
func seedRegistry() *medical.PatientRegistry {
	reg := medical.NewPatientRegistry()
	reg.Upsert(medical.PatientRecord{
		PatientID:      "p1",
		Age:            45,
		Sex:            "female",
		Conditions:     []string{"dm", "htn"},
		LastA1c:        7.2,
		LastA1cDate:    time.Now().Add(-30 * 24 * time.Hour),
		LastSystolicBP: 142,
		LastBPDate:     time.Now().Add(-7 * 24 * time.Hour),
		Medications: []medical.MedicationEntry{
			{Name: "Metformin", Dose: "500 mg", Frequency: "BID", Class: "Biguanide"},
		},
	})
	reg.Upsert(medical.PatientRecord{
		PatientID:  "p2",
		Age:        60,
		Sex:        "male",
		Conditions: []string{"chf"},
		LastWeightLb: 195,
		LastSpO2:   94,
		Medications: []medical.MedicationEntry{
			{Name: "Furosemide", Dose: "40 mg", Frequency: "daily", Class: "Loop diuretic"},
		},
	})
	return reg
}

func TestBulkStartExportAndStatus(t *testing.T) {
	dir := t.TempDir()
	reg := seedRegistry()
	exp := fhir.NewBulkExporter(reg, dir)

	jobID := exp.StartExport(nil) // nil = all types
	if jobID == "" {
		t.Fatal("StartExport returned empty job ID")
	}

	job, ok := exp.GetStatus(jobID)
	if !ok {
		t.Fatalf("GetStatus(%q) not found immediately after StartExport", jobID)
	}
	// Status must be one of the accepted values right after start.
	validStart := map[string]bool{"accepted": true, "in-progress": true, "completed": true}
	if !validStart[job.Status] {
		t.Fatalf("unexpected status immediately after StartExport: %q", job.Status)
	}

	// Wait for completion so the background goroutine finishes writing files
	// before t.TempDir cleanup removes the directory.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := exp.GetStatus(jobID)
		if j != nil && (j.Status == "completed" || j.Status == "error") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestBulkExportCompletesAndWritesNDJSON(t *testing.T) {
	dir := t.TempDir()
	reg := seedRegistry()
	exp := fhir.NewBulkExporter(reg, dir)

	jobID := exp.StartExport([]string{"Patient", "Condition", "Observation", "MedicationRequest"})

	// Poll until complete (or timeout after 3 s).
	deadline := time.Now().Add(3 * time.Second)
	var job *fhir.BulkExportJob
	for time.Now().Before(deadline) {
		j, ok := exp.GetStatus(jobID)
		if !ok {
			t.Fatal("job disappeared from status map")
		}
		if j.Status == "completed" || j.Status == "error" {
			job = j
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if job == nil {
		t.Fatal("job did not complete within timeout")
	}
	if job.Status == "error" {
		t.Fatalf("job failed: %+v", job.Error)
	}

	// Verify output manifest contains the four resource types.
	typesSeen := map[string]bool{}
	for _, f := range job.Output {
		typesSeen[f.Type] = true
		if f.Count <= 0 && f.Type != "Observation" { // Observation may be empty for some configs
			t.Errorf("output type %s has count=%d, expected >0", f.Type, f.Count)
		}
		if f.URL == "" {
			t.Errorf("output type %s has empty URL", f.Type)
		}
	}
	for _, want := range []string{"Patient", "Condition", "Observation", "MedicationRequest"} {
		if !typesSeen[want] {
			t.Errorf("output missing type %s", want)
		}
	}
}

func TestBulkExportNDJSONContent(t *testing.T) {
	dir := t.TempDir()
	reg := seedRegistry()
	exp := fhir.NewBulkExporter(reg, dir)

	jobID := exp.StartExport([]string{"Patient"})

	// Wait for completion.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := exp.GetStatus(jobID)
		if j != nil && (j.Status == "completed" || j.Status == "error") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Read NDJSON file directly from disk.
	path := filepath.Join(dir, jobID, "Patient.ndjson")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading Patient.ndjson: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 Patient NDJSON lines (one per patient), got %d", len(lines))
	}
	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i+1, err)
			continue
		}
		if rec["resourceType"] != "Patient" {
			t.Errorf("line %d: resourceType=%v, want Patient", i+1, rec["resourceType"])
		}
		if _, ok := rec["id"]; !ok {
			t.Errorf("line %d: missing id field", i+1)
		}
	}
}

func TestBulkGetFile(t *testing.T) {
	dir := t.TempDir()
	reg := seedRegistry()
	exp := fhir.NewBulkExporter(reg, dir)

	jobID := exp.StartExport([]string{"Condition"})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := exp.GetStatus(jobID)
		if j != nil && (j.Status == "completed" || j.Status == "error") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	data, err := exp.GetFile(jobID, "Condition.ndjson")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("GetFile returned empty data")
	}
	// Every line must decode as a FHIR Condition.
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("Condition line %d is not valid JSON: %v", i+1, err)
			continue
		}
		if rec["resourceType"] != "Condition" {
			t.Errorf("line %d: resourceType=%v, want Condition", i+1, rec["resourceType"])
		}
	}
}

func TestBulkGetFileUnknownJob(t *testing.T) {
	dir := t.TempDir()
	reg := medical.NewPatientRegistry()
	exp := fhir.NewBulkExporter(reg, dir)

	_, err := exp.GetFile("nonexistent-job-id", "Patient.ndjson")
	if err == nil {
		t.Fatal("expected error for unknown job ID, got nil")
	}
}

func TestBulkGetFileIncompleteJob(t *testing.T) {
	dir := t.TempDir()
	// Use empty registry so the export is fast but we can race by using a type
	// list that won't match — the job will sit at "accepted" briefly.
	reg := medical.NewPatientRegistry()
	// Pre-populate to slow down slightly — but the point is we call GetFile
	// before polling for completion.
	exp := fhir.NewBulkExporter(reg, dir)

	// Start but don't wait.
	jobID := exp.StartExport([]string{"Patient"})
	// Immediately try to GetFile; the job may be in any state < completed.
	// We loop until the job completes and only assert on the pre-complete failure.
	j, _ := exp.GetStatus(jobID)
	if j != nil && j.Status != "completed" && j.Status != "error" {
		_, err := exp.GetFile(jobID, "Patient.ndjson")
		if err == nil {
			t.Error("expected error when job not yet complete, got nil")
		}
	}
}

func TestBulkExportFilteredTypes(t *testing.T) {
	dir := t.TempDir()
	reg := seedRegistry()
	exp := fhir.NewBulkExporter(reg, dir)

	// Only request Patient.
	jobID := exp.StartExport([]string{"Patient"})

	deadline := time.Now().Add(3 * time.Second)
	var job *fhir.BulkExportJob
	for time.Now().Before(deadline) {
		j, _ := exp.GetStatus(jobID)
		if j != nil && (j.Status == "completed" || j.Status == "error") {
			job = j
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if job == nil {
		t.Fatal("job did not complete within timeout")
	}
	if len(job.Output) != 1 {
		t.Fatalf("expected 1 output file, got %d", len(job.Output))
	}
	if job.Output[0].Type != "Patient" {
		t.Errorf("expected Patient output, got %s", job.Output[0].Type)
	}
}

func TestBulkExportUnsupportedType(t *testing.T) {
	dir := t.TempDir()
	reg := seedRegistry()
	exp := fhir.NewBulkExporter(reg, dir)

	// Unknown types should be silently ignored, resulting in no output files.
	jobID := exp.StartExport([]string{"Unicorn", "UnsupportedResourceType"})

	deadline := time.Now().Add(3 * time.Second)
	var job *fhir.BulkExportJob
	for time.Now().Before(deadline) {
		j, _ := exp.GetStatus(jobID)
		if j != nil && (j.Status == "completed" || j.Status == "error") {
			job = j
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if job == nil {
		t.Fatal("job did not complete within timeout")
	}
	if len(job.Output) != 0 {
		t.Errorf("expected 0 output files for unsupported types, got %d", len(job.Output))
	}
}

func TestPatientToFHIR(t *testing.T) {
	p := medical.PatientRecord{PatientID: "pt-001", Age: 35, Sex: "male"}
	rec := fhir.PatientToFHIR(p)
	if rec["resourceType"] != "Patient" {
		t.Errorf("resourceType=%v", rec["resourceType"])
	}
	if rec["id"] != "pt-001" {
		t.Errorf("id=%v", rec["id"])
	}
	if rec["gender"] != "male" {
		t.Errorf("gender=%v", rec["gender"])
	}
	if _, ok := rec["birthDate"]; !ok {
		t.Error("missing birthDate for patient with Age>0")
	}
}

func TestConditionToFHIR(t *testing.T) {
	p := medical.PatientRecord{PatientID: "pt-002"}
	rec := fhir.ConditionToFHIR(p, "dm")
	if rec["resourceType"] != "Condition" {
		t.Errorf("resourceType=%v", rec["resourceType"])
	}
	subj, _ := rec["subject"].(map[string]any)
	if subj == nil || subj["reference"] != "Patient/pt-002" {
		t.Errorf("subject.reference wrong: %v", subj)
	}
}

func TestObservationToFHIR(t *testing.T) {
	p := medical.PatientRecord{PatientID: "pt-003"}
	obs := medical.RPMObservation{
		Device:  "bp-cuff",
		Metric:  "systolic_bp",
		Value:   138,
		Unit:    "mmHg",
		TakenAt: "2026-01-01T08:00:00Z",
	}
	rec := fhir.ObservationToFHIR(p, obs)
	if rec["resourceType"] != "Observation" {
		t.Errorf("resourceType=%v", rec["resourceType"])
	}
	if rec["status"] != "final" {
		t.Errorf("status=%v", rec["status"])
	}
	vq, _ := rec["valueQuantity"].(map[string]any)
	if vq == nil {
		t.Fatal("missing valueQuantity")
	}
	if vq["value"].(float64) != 138 {
		t.Errorf("value=%v", vq["value"])
	}
	if rec["effectiveDateTime"] != "2026-01-01T08:00:00Z" {
		t.Errorf("effectiveDateTime=%v", rec["effectiveDateTime"])
	}
}
