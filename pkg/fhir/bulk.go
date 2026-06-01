package fhir

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/google/uuid"
)

// BulkExportJob tracks the lifecycle of a FHIR Bulk Data export operation.
// Status values follow the HL7 FHIR Bulk Data IG:
//
//	"accepted"    — job received but not yet started
//	"in-progress" — background goroutine is writing NDJSON files
//	"completed"   — all files written; Output is populated
//	"error"       — export failed; Error is populated
type BulkExportJob struct {
	ID                  string      `json:"id"`
	Status              string      `json:"status"`
	Request             string      `json:"request"`
	RequiresAccessToken bool        `json:"requiresAccessToken"`
	Output              []BulkFile  `json:"output,omitempty"`
	Error               []BulkError `json:"error,omitempty"`
	TransactionTime     string      `json:"transactionTime"`
	Created             time.Time   `json:"created"`
	Completed           time.Time   `json:"completed,omitempty"`
}

// BulkFile describes one NDJSON output file in the export manifest.
type BulkFile struct {
	Type  string `json:"type"`  // FHIR resource type, e.g. "Patient"
	URL   string `json:"url"`   // download URL for the NDJSON file
	Count int    `json:"count"` // number of resources in the file
}

// BulkError describes an error that occurred while exporting a resource type.
type BulkError struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

// supportedTypes is the set of FHIR resource types the exporter handles.
var supportedTypes = map[string]bool{
	"Patient":           true,
	"Observation":       true,
	"Condition":         true,
	"MedicationRequest": true,
}

// BulkExporter manages FHIR Bulk Data export jobs. It writes NDJSON files to
// outputDir/{jobID}/{Type}.ndjson and keeps an in-memory job registry for
// status polling.
type BulkExporter struct {
	jobs      map[string]*BulkExportJob
	mu        sync.RWMutex
	registry  *medical.PatientRegistry
	outputDir string
}

// NewBulkExporter creates a BulkExporter backed by the given PatientRegistry.
// outputDir is the filesystem path where NDJSON files are written.
func NewBulkExporter(registry *medical.PatientRegistry, outputDir string) *BulkExporter {
	return &BulkExporter{
		jobs:      make(map[string]*BulkExportJob),
		registry:  registry,
		outputDir: outputDir,
	}
}

// StartExport kicks off an asynchronous export of the requested resource types
// and returns the job ID. If types is empty all supported types are exported.
// The caller should poll GetStatus(jobID) until status is "completed" or "error".
func (e *BulkExporter) StartExport(types []string) string {
	id := uuid.New().String()
	now := time.Now().UTC()

	// Resolve the set of types to export.
	exportTypes := resolveTypes(types)

	job := &BulkExportJob{
		ID:              id,
		Status:          "accepted",
		Request:         "/$export",
		TransactionTime: now.Format(time.RFC3339),
		Created:         now,
	}

	e.mu.Lock()
	e.jobs[id] = job
	e.mu.Unlock()

	go e.runExport(job, exportTypes)
	return id
}

// GetStatus returns the job by ID.
func (e *BulkExporter) GetStatus(jobID string) (*BulkExportJob, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	job, ok := e.jobs[jobID]
	if !ok {
		return nil, false
	}
	// Return a copy so the caller cannot mutate internal state.
	cp := *job
	return &cp, true
}

// GetFile returns the raw bytes of a previously exported NDJSON file.
func (e *BulkExporter) GetFile(jobID, filename string) ([]byte, error) {
	// Validate that the job exists and is complete.
	e.mu.RLock()
	job, ok := e.jobs[jobID]
	e.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("job %s not found", jobID)
	}
	if job.Status != "completed" {
		return nil, fmt.Errorf("job %s is not complete (status=%s)", jobID, job.Status)
	}

	path := filepath.Join(e.outputDir, jobID, filepath.Base(filename))
	return os.ReadFile(path)
}

// runExport is the background goroutine that performs the actual export.
func (e *BulkExporter) runExport(job *BulkExportJob, types []string) {
	e.setStatus(job.ID, "in-progress")

	dir := filepath.Join(e.outputDir, job.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		e.failJob(job.ID, fmt.Errorf("mkdir %s: %w", dir, err))
		return
	}

	patients := e.registry.List()
	var output []BulkFile
	var errs []BulkError

	for _, resType := range types {
		file, err := e.exportType(dir, job.ID, resType, patients)
		if err != nil {
			errs = append(errs, BulkError{Type: resType})
			continue
		}
		output = append(output, file)
	}

	e.mu.Lock()
	j := e.jobs[job.ID]
	if j != nil {
		j.Status = "completed"
		j.Output = output
		j.Error = errs
		j.Completed = time.Now().UTC()
	}
	e.mu.Unlock()
}

// exportType writes one NDJSON file and returns its BulkFile descriptor.
func (e *BulkExporter) exportType(dir, jobID, resType string, patients []medical.PatientRecord) (BulkFile, error) {
	path := filepath.Join(dir, resType+".ndjson")
	f, err := os.Create(path)
	if err != nil {
		return BulkFile{}, err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	count := 0

	for _, p := range patients {
		switch resType {
		case "Patient":
			rec := PatientToFHIR(p)
			if err := enc.Encode(rec); err != nil {
				return BulkFile{}, err
			}
			count++

		case "Condition":
			for _, cond := range p.Conditions {
				rec := ConditionToFHIR(p, cond)
				if err := enc.Encode(rec); err != nil {
					return BulkFile{}, err
				}
				count++
			}

		case "Observation":
			// Export the most recent vital observations stored on the record.
			for _, obs := range rpmObservationsFromRecord(p) {
				rec := ObservationToFHIR(p, obs)
				if err := enc.Encode(rec); err != nil {
					return BulkFile{}, err
				}
				count++
			}

		case "MedicationRequest":
			for _, med := range p.Medications {
				rec := MedicationRequestToFHIR(p, med)
				if err := enc.Encode(rec); err != nil {
					return BulkFile{}, err
				}
				count++
			}
		}
	}

	return BulkFile{
		Type:  resType,
		URL:   "/fhir/bulk/" + jobID + "/" + resType + ".ndjson",
		Count: count,
	}, nil
}

// setStatus transitions a job to a new status.
func (e *BulkExporter) setStatus(jobID, status string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if j, ok := e.jobs[jobID]; ok {
		j.Status = status
	}
}

// failJob marks a job as errored with a diagnostic error entry.
func (e *BulkExporter) failJob(jobID string, cause error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if j, ok := e.jobs[jobID]; ok {
		j.Status = "error"
		j.Error = append(j.Error, BulkError{Type: cause.Error()})
		j.Completed = time.Now().UTC()
	}
}

// resolveTypes normalises the caller-supplied list, falling back to all
// supported types when none are requested.
func resolveTypes(requested []string) []string {
	if len(requested) == 0 {
		return []string{"Patient", "Observation", "Condition", "MedicationRequest"}
	}
	out := make([]string, 0, len(requested))
	for _, t := range requested {
		if supportedTypes[t] {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		// None of the requested types are supported — export nothing.
		return []string{}
	}
	return out
}

// rpmObservationsFromRecord synthesises RPMObservation values from the
// scalar fields on PatientRecord so they can be rendered as FHIR Observations.
func rpmObservationsFromRecord(p medical.PatientRecord) []medical.RPMObservation {
	var out []medical.RPMObservation
	if p.LastSystolicBP > 0 {
		out = append(out, medical.RPMObservation{
			Device:  "fhir-export",
			Metric:  "systolic_bp",
			Value:   p.LastSystolicBP,
			Unit:    "mmHg",
			TakenAt: p.LastBPDate.Format(time.RFC3339),
		})
	}
	if p.LastWeightLb > 0 {
		out = append(out, medical.RPMObservation{
			Device:  "fhir-export",
			Metric:  "weight_lb",
			Value:   p.LastWeightLb,
			Unit:    "lb",
		})
	}
	if p.LastSpO2 > 0 {
		out = append(out, medical.RPMObservation{
			Device:  "fhir-export",
			Metric:  "spo2",
			Value:   p.LastSpO2,
			Unit:    "%",
		})
	}
	if p.LastA1c > 0 {
		out = append(out, medical.RPMObservation{
			Device:  "fhir-export",
			Metric:  "hba1c",
			Value:   p.LastA1c,
			Unit:    "%",
			TakenAt: p.LastA1cDate.Format(time.RFC3339),
		})
	}
	return out
}

// PatientToFHIR converts a PatientRecord into a FHIR R4 Patient resource map.
func PatientToFHIR(p medical.PatientRecord) map[string]any {
	birthYear := 0
	if p.Age > 0 {
		birthYear = time.Now().Year() - p.Age
	}
	rec := map[string]any{
		"resourceType": "Patient",
		"id":           p.PatientID,
	}
	if p.Sex != "" {
		rec["gender"] = p.Sex
	}
	if birthYear > 0 {
		rec["birthDate"] = fmt.Sprintf("%d-01-01", birthYear)
	}
	return rec
}

// ConditionToFHIR converts a condition string on a PatientRecord into a FHIR
// R4 Condition resource map.
func ConditionToFHIR(p medical.PatientRecord, condition string) map[string]any {
	return map[string]any{
		"resourceType": "Condition",
		"id":           p.PatientID + "-cond-" + condition,
		"subject":      map[string]any{"reference": "Patient/" + p.PatientID},
		"code": map[string]any{
			"text": condition,
			"coding": []map[string]any{
				{
					"system":  "http://snomed.info/sct",
					"display": condition,
				},
			},
		},
		"clinicalStatus": map[string]any{
			"coding": []map[string]any{
				{
					"system": "http://terminology.hl7.org/CodeSystem/condition-clinical",
					"code":   "active",
				},
			},
		},
	}
}

// ObservationToFHIR converts an RPMObservation into a FHIR R4 Observation
// resource map. The LOINC code is inferred from the metric name.
func ObservationToFHIR(p medical.PatientRecord, obs medical.RPMObservation) map[string]any {
	id := p.PatientID + "-obs-" + obs.Metric
	loincCode, loincDisplay := metricToLOINC(obs.Metric)

	result := map[string]any{
		"resourceType": "Observation",
		"id":           id,
		"status":       "final",
		"subject":      map[string]any{"reference": "Patient/" + p.PatientID},
		"code": map[string]any{
			"text": obs.Metric,
			"coding": []map[string]any{
				{
					"system":  "http://loinc.org",
					"code":    loincCode,
					"display": loincDisplay,
				},
			},
		},
		"valueQuantity": map[string]any{
			"value": obs.Value,
			"unit":  obs.Unit,
		},
	}
	if obs.TakenAt != "" {
		result["effectiveDateTime"] = obs.TakenAt
	}
	return result
}

// MedicationRequestToFHIR converts a MedicationEntry into a FHIR R4
// MedicationRequest resource map.
func MedicationRequestToFHIR(p medical.PatientRecord, med medical.MedicationEntry) map[string]any {
	return map[string]any{
		"resourceType": "MedicationRequest",
		"id":           p.PatientID + "-medrx-" + med.Name,
		"status":       "active",
		"intent":       "order",
		"subject":      map[string]any{"reference": "Patient/" + p.PatientID},
		"medicationCodeableConcept": map[string]any{
			"text": med.Name,
		},
		"dosageInstruction": []map[string]any{
			{
				"text": med.Dose,
			},
		},
	}
}

// metricToLOINC maps known RPM metric names to LOINC codes.
func metricToLOINC(metric string) (code, display string) {
	switch metric {
	case "systolic_bp":
		return "8480-6", "Systolic blood pressure"
	case "weight_lb":
		return "29463-7", "Body weight"
	case "spo2":
		return "59408-5", "Oxygen saturation in Arterial blood by Pulse oximetry"
	case "heart_rate":
		return "8867-4", "Heart rate"
	case "hba1c":
		return "4548-4", "Hemoglobin A1c/Hemoglobin.total in Blood"
	case "glucose":
		return "2339-0", "Glucose [Mass/volume] in Blood"
	default:
		return "unknown", metric
	}
}
