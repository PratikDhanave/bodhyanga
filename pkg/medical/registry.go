package medical

import (
	"sync"
	"time"
)

// PatientRecord is a population-health snapshot of one patient.
// Demo-grade: enough fields to run panel rules and outreach without PHI.
type PatientRecord struct {
	PatientID      string            `json:"patient_id"`
	Age            int               `json:"age,omitempty"`
	Sex            string            `json:"sex,omitempty"`
	Conditions     []string          `json:"conditions,omitempty"`
	LastVisit      time.Time         `json:"last_visit,omitempty"`
	LastA1c        float64           `json:"last_a1c,omitempty"`
	LastA1cDate    time.Time         `json:"last_a1c_date,omitempty"`
	LastSystolicBP float64           `json:"last_systolic_bp,omitempty"`
	LastBPDate     time.Time         `json:"last_bp_date,omitempty"`
	LastWeightLb   float64           `json:"last_weight_lb,omitempty"`
	LastSpO2       float64           `json:"last_spo2,omitempty"`
	LastACTScore   float64           `json:"last_act_score,omitempty"`
	LastACTDate    time.Time         `json:"last_act_date,omitempty"`
	LastPHQ9Date   time.Time         `json:"last_phq9_date,omitempty"`
	Medications    []MedicationEntry `json:"medications,omitempty"`
	RPMEnrolled    bool              `json:"rpm_enrolled,omitempty"`
	DischargedAt   time.Time         `json:"discharged_at,omitempty"`
}

// PatientRegistry is a thread-safe in-memory roster used by the panel manager.
type PatientRegistry struct {
	mu       sync.RWMutex
	patients map[string]PatientRecord
}

// NewPatientRegistry returns an empty registry.
func NewPatientRegistry() *PatientRegistry {
	return &PatientRegistry{patients: make(map[string]PatientRecord)}
}

// Upsert inserts or replaces a record by PatientID.
func (r *PatientRegistry) Upsert(p PatientRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patients[p.PatientID] = p
}

// Get returns a record by ID.
func (r *PatientRegistry) Get(id string) (PatientRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.patients[id]
	return p, ok
}

// List returns a snapshot of every record.
func (r *PatientRegistry) List() []PatientRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PatientRecord, 0, len(r.patients))
	for _, p := range r.patients {
		out = append(out, p)
	}
	return out
}

// FilterByCondition returns records that include the given condition code.
func (r *PatientRegistry) FilterByCondition(code string) []PatientRecord {
	out := []PatientRecord{}
	for _, p := range r.List() {
		for _, c := range p.Conditions {
			if c == code {
				out = append(out, p)
				break
			}
		}
	}
	return out
}
