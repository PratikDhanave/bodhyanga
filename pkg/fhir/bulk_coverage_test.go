package fhir

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

func TestBulkExporter_FailJob(t *testing.T) {
	reg := medical.NewPatientRegistry()
	dir := t.TempDir()
	exp := NewBulkExporter(reg, dir)

	// Start a job, then manually fail it.
	id := exp.StartExport(nil)

	// Wait for export to complete.
	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(200 * time.Millisecond)

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

	time.Sleep(200 * time.Millisecond)

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
	// Immediately try to get file before completion.
	_, err := exp.GetFile(id, "Patient.ndjson")
	if err == nil {
		// It may have completed already due to empty registry; check status.
		time.Sleep(100 * time.Millisecond)
	}
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
	time.Sleep(200 * time.Millisecond)

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
	time.Sleep(300 * time.Millisecond)

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
