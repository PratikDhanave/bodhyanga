package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestFHIRQuery_KnownPatientReturnsRecord(t *testing.T) {
	raw, err := FHIRQuery{}.Invoke(context.Background(),
		json.RawMessage(`{"resource":"Patient","patient_id":"pt-001"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var p fhirPatient
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, string(raw))
	}
	if !p.Found {
		t.Fatalf("found=false, want true for pt-001")
	}
	if p.Name == "" || p.DOB == "" {
		t.Fatalf("missing demographics: %+v", p)
	}
	if len(p.Conditions) == 0 || len(p.Medications) == 0 {
		t.Fatalf("expected conditions and medications populated, got %+v", p)
	}
	if got, ok := p.Metadata["source"]; !ok || got != "demo-synth" {
		t.Fatalf("metadata.source=%v, want demo-synth", p.Metadata["source"])
	}
}

func TestFHIRQuery_UnknownPatientReturnsMiss(t *testing.T) {
	raw, err := FHIRQuery{}.Invoke(context.Background(),
		json.RawMessage(`{"resource":"Patient","patient_id":"pt-999"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var m fhirMiss
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode miss: %v\nbody=%s", err, string(raw))
	}
	if m.Found {
		t.Fatalf("found=true on miss, want false")
	}
	if m.ID != "pt-999" {
		t.Fatalf("id=%q, want pt-999", m.ID)
	}
	if m.Metadata["source"] != "demo-synth" {
		t.Fatalf("metadata.source=%v, want demo-synth", m.Metadata["source"])
	}
}

func TestFHIRQuery_UnsupportedResourceErrors(t *testing.T) {
	_, err := FHIRQuery{}.Invoke(context.Background(),
		json.RawMessage(`{"resource":"Observation","patient_id":"pt-001"}`))
	if err == nil {
		t.Fatalf("expected error on unsupported resource")
	}
	if !strings.Contains(err.Error(), "unsupported resource") {
		t.Fatalf("error=%q, want 'unsupported resource'", err.Error())
	}
}

func TestFHIRQuery_EmptyPatientIDErrors(t *testing.T) {
	_, err := FHIRQuery{}.Invoke(context.Background(),
		json.RawMessage(`{"resource":"Patient","patient_id":""}`))
	if err == nil {
		t.Fatalf("expected error on empty patient_id")
	}
}
