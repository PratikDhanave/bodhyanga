package fhir

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// waitForTerminal blocks until the export job reaches a terminal state
// (completed or error). runExport sets the terminal status as its final
// action — after every file write and Close — so once terminal, the
// background goroutine no longer touches the export directory. Waiting on it
// (rather than a fixed time.Sleep) keeps the goroutine's writes from racing
// t.TempDir()'s RemoveAll cleanup, which flakes on loaded CI runners.
func waitForTerminal(t *testing.T, exp *BulkExporter, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if job, ok := exp.GetStatus(id); ok && (job.Status == "completed" || job.Status == "error") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("export %s did not reach a terminal state in time", id)
}

func TestBulkExporter_FailJob(t *testing.T) {
	reg := medical.NewPatientRegistry()
	dir := t.TempDir()
	exp := NewBulkExporter(reg, dir)

	// Start a job and let the background export finish first, so it can't
	// overwrite the error status we set below (and so its writes don't race
	// TempDir cleanup).
	id := exp.StartExport(nil)
	waitForTerminal(t, exp, id)

	// Call failJob directly.
	exp.failJob(id, fmt.Errorf("test failure"))

	job, ok := exp.GetStatus(id)
	if !ok {
		t.Fatal("expected job to exist")
	}
	if job.Status != "error" {
		t.Errorf("expected error status, got %q", job.Status)
	}
	if len(job.Error) == 0 {
		t.Error("expected error entries")
	}
}

func TestBulkExporter_UnknownResourceType(t *testing.T) {
	reg := medical.NewPatientRegistry()
	dir := t.TempDir()
	exp := NewBulkExporter(reg, dir)

	// Request only unsupported types.
	id := exp.StartExport([]string{"FakeResource"})

	waitForTerminal(t, exp, id)

	job, ok := exp.GetStatus(id)
	if !ok {
		t.Fatal("expected job to exist")
	}
	if job.Status != "completed" {
		t.Errorf("expected completed status, got %q", job.Status)
	}
	// No output files since no valid types.
	if len(job.Output) != 0 {
		t.Errorf("expected no output, got %d", len(job.Output))
	}
}

func TestBulkExporter_EmptyRegistry(t *testing.T) {
	reg := medical.NewPatientRegistry()
	dir := t.TempDir()
	exp := NewBulkExporter(reg, dir)

	id := exp.StartExport([]string{"Patient"})

	waitForTerminal(t, exp, id)

	job, ok := exp.GetStatus(id)
	if !ok {
		t.Fatal("expected job to exist")
	}
	if job.Status != "completed" {
		t.Errorf("expected completed, got %q", job.Status)
	}
	if len(job.Output) != 1 {
		t.Fatalf("expected 1 output, got %d", len(job.Output))
	}
	if job.Output[0].Count != 0 {
		t.Errorf("expected 0 count, got %d", job.Output[0].Count)
	}
}

func TestBulkExporter_GetStatus_NotFound(t *testing.T) {
	reg := medical.NewPatientRegistry()
	dir := t.TempDir()
	exp := NewBulkExporter(reg, dir)

	_, ok := exp.GetStatus("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestBulkExporter_GetFile_JobNotFound(t *testing.T) {
	reg := medical.NewPatientRegistry()
	dir := t.TempDir()
	exp := NewBulkExporter(reg, dir)

	_, err := exp.GetFile("nonexistent", "Patient.ndjson")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}
}

func TestBulkExporter_GetFile_NotComplete(t *testing.T) {
	reg := medical.NewPatientRegistry()
	dir := t.TempDir()
	exp := NewBulkExporter(reg, dir)

	id := exp.StartExport(nil)
	// Try to get the file right after starting. Depending on scheduling the
	// export may or may not have completed, so either a "not complete" error
	// or a successful read is acceptable — the point is exercising GetFile
	// before completion is observed.
	_, _ = exp.GetFile(id, "Patient.ndjson")
	// Wait for the export to finish so its writes don't race TempDir cleanup.
	waitForTerminal(t, exp, id)
}

func TestBulkExporter_RunExport_MkdirFails(t *testing.T) {
	reg := medical.NewPatientRegistry()
	// Use a path that will fail (file instead of directory parent).
	tmpFile := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	exp := NewBulkExporter(reg, filepath.Join(tmpFile, "subdir"))

	id := exp.StartExport([]string{"Patient"})
	waitForTerminal(t, exp, id)

	job, ok := exp.GetStatus(id)
	if !ok {
		t.Fatal("expected job to exist")
	}
	if job.Status != "error" {
		t.Errorf("expected error status, got %q", job.Status)
	}
}

func TestBulkExporter_AllResourceTypes(t *testing.T) {
	reg := medical.NewPatientRegistry()
	bpDate := time.Now().UTC()
	a1cDate := time.Now().UTC()
	reg.Upsert(medical.PatientRecord{
		PatientID:     "pt-test",
		Age:           50,
		Sex:           "male",
		Conditions:    []string{"hypertension"},
		Medications:   []medical.MedicationEntry{{Name: "lisinopril", Dose: "10mg"}},
		LastSystolicBP: 140,
		LastBPDate:    bpDate,
		LastWeightLb:  180,
		LastSpO2:      95,
		LastA1c:       7.0,
		LastA1cDate:   a1cDate,
	})

	dir := t.TempDir()
	exp := NewBulkExporter(reg, dir)

	id := exp.StartExport(nil) // all types
	waitForTerminal(t, exp, id)

	job, ok := exp.GetStatus(id)
	if !ok {
		t.Fatal("expected job to exist")
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed, got %q", job.Status)
	}
	if len(job.Output) != 4 {
		t.Fatalf("expected 4 outputs, got %d", len(job.Output))
	}

	// GetFile for Patient.ndjson.
	data, err := exp.GetFile(id, "Patient.ndjson")
	if err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty Patient.ndjson")
	}
}

func TestResolveTypes_Empty(t *testing.T) {
	types := resolveTypes(nil)
	if len(types) != 4 {
		t.Errorf("expected 4 default types, got %d", len(types))
	}
}

func TestResolveTypes_ValidSubset(t *testing.T) {
	types := resolveTypes([]string{"Patient", "Condition"})
	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}
}

func TestResolveTypes_AllInvalid(t *testing.T) {
	types := resolveTypes([]string{"Fake1", "Fake2"})
	if len(types) != 0 {
		t.Errorf("expected 0 types, got %d", len(types))
	}
}
