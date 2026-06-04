package fhir

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// ---------------------------------------------------------------------------
// ConditionCode
// ---------------------------------------------------------------------------

func TestConditionCode_WithCoding(t *testing.T) {
	c := Condition{
		Code: CodeableConcept{
			Coding: []Coding{{Code: "J45.20"}},
			Text:   "asthma",
		},
	}
	got := ConditionCode(c)
	if got != "j45.20" {
		t.Fatalf("expected j45.20, got %q", got)
	}
}

func TestConditionCode_FallbackToText(t *testing.T) {
	c := Condition{
		Code: CodeableConcept{
			Coding: []Coding{{Code: ""}},
			Text:   "Asthma",
		},
	}
	got := ConditionCode(c)
	if got != "asthma" {
		t.Fatalf("expected asthma, got %q", got)
	}
}

func TestConditionCode_EmptyCoding(t *testing.T) {
	c := Condition{
		Code: CodeableConcept{
			Text: "COPD",
		},
	}
	got := ConditionCode(c)
	if got != "copd" {
		t.Fatalf("expected copd, got %q", got)
	}
}

func TestConditionCode_BothEmpty(t *testing.T) {
	c := Condition{
		Code: CodeableConcept{},
	}
	got := ConditionCode(c)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// CarePlanToObservations
// ---------------------------------------------------------------------------

func TestCarePlanToObservations(t *testing.T) {
	now := time.Now().UTC()
	plan := medical.CarePlan{
		PatientID: "pt-001",
		CreatedAt: now,
		Goals: []medical.GoalEntry{
			{Metric: "systolic_bp", Target: 120, Unit: "mmHg"},
			{Metric: "hba1c", Target: 7.0, Unit: "%"},
		},
	}
	obs := CarePlanToObservations(plan)
	if len(obs) != 2 {
		t.Fatalf("expected 2 observations, got %d", len(obs))
	}
	if obs[0].ResourceType != "Observation" {
		t.Errorf("expected ResourceType=Observation, got %q", obs[0].ResourceType)
	}
	if obs[0].Code.Text != "systolic_bp" {
		t.Errorf("expected code text systolic_bp, got %q", obs[0].Code.Text)
	}
	if obs[0].ValueQty == nil || obs[0].ValueQty.Value != 120 {
		t.Errorf("expected value 120, got %v", obs[0].ValueQty)
	}
	if obs[1].Code.Text != "hba1c" {
		t.Errorf("expected code text hba1c, got %q", obs[1].Code.Text)
	}
}

func TestCarePlanToObservations_Empty(t *testing.T) {
	plan := medical.CarePlan{PatientID: "pt-001"}
	obs := CarePlanToObservations(plan)
	if len(obs) != 0 {
		t.Fatalf("expected 0 observations, got %d", len(obs))
	}
}

// ---------------------------------------------------------------------------
// Marshal
// ---------------------------------------------------------------------------

func TestMarshal(t *testing.T) {
	out := Marshal(map[string]string{"key": "value"})
	if out == "" {
		t.Fatal("expected non-empty JSON string")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("Marshal returned invalid JSON: %v", err)
	}
	if m["key"] != "value" {
		t.Errorf("expected key=value, got %q", m["key"])
	}
}

// ---------------------------------------------------------------------------
// observationMetric — cover all text matches and LOINC code matches
// ---------------------------------------------------------------------------

func TestObservationMetric_AllTextMatches(t *testing.T) {
	tests := []struct {
		text   string
		expect string
	}{
		{"systolic blood pressure", "systolic_bp"},
		{"systolic_bp", "systolic_bp"},
		{"body weight", "weight_lb"},
		{"weight", "weight_lb"},
		{"oxygen saturation", "spo2"},
		{"spo2", "spo2"},
		{"heart rate", "heart_rate"},
		{"hemoglobin a1c", "hba1c"},
		{"hba1c", "hba1c"},
		{"glucose", "glucose"},
	}
	for _, tt := range tests {
		cc := CodeableConcept{Text: tt.text}
		got := observationMetric(cc)
		if got != tt.expect {
			t.Errorf("text=%q: expected %q, got %q", tt.text, tt.expect, got)
		}
	}
}

func TestObservationMetric_LOINCCodes(t *testing.T) {
	tests := []struct {
		code   string
		expect string
	}{
		{"8480-6", "systolic_bp"},
		{"29463-7", "weight_lb"},
		{"59408-5", "spo2"},
		{"2708-6", "spo2"},
		{"8867-4", "heart_rate"},
		{"4548-4", "hba1c"},
		{"2339-0", "glucose"},
	}
	for _, tt := range tests {
		cc := CodeableConcept{
			Text:   "unknown",
			Coding: []Coding{{Code: tt.code}},
		}
		got := observationMetric(cc)
		if got != tt.expect {
			t.Errorf("code=%q: expected %q, got %q", tt.code, tt.expect, got)
		}
	}
}

func TestObservationMetric_Unknown(t *testing.T) {
	cc := CodeableConcept{
		Text:   "something unknown",
		Coding: []Coding{{Code: "99999-9"}},
	}
	got := observationMetric(cc)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// ObservationToRPM — nil ValueQty + unknown metric
// ---------------------------------------------------------------------------

func TestObservationToRPM_NilValueQty(t *testing.T) {
	o := Observation{
		Code:     CodeableConcept{Text: "systolic blood pressure"},
		ValueQty: nil,
	}
	_, ok := ObservationToRPM(o)
	if ok {
		t.Fatal("expected false for nil ValueQty")
	}
}

func TestObservationToRPM_UnknownMetric(t *testing.T) {
	o := Observation{
		Code:     CodeableConcept{Text: "unknown metric"},
		ValueQty: &Quantity{Value: 42, Unit: "?"},
	}
	_, ok := ObservationToRPM(o)
	if ok {
		t.Fatal("expected false for unknown metric")
	}
}

// ---------------------------------------------------------------------------
// EncounterToADT — default/unknown status
// ---------------------------------------------------------------------------

func TestEncounterToADT_UnknownStatus(t *testing.T) {
	e := Encounter{Status: "planned"}
	_, ok := EncounterToADT(e)
	if ok {
		t.Fatal("expected false for planned status")
	}
}

func TestEncounterToADT_FinishedWithEndPeriod(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	e := Encounter{
		Status:  "finished",
		Subject: Reference{Reference: "Patient/pt-001"},
		Period:  Period{Start: now, End: now},
	}
	adt, ok := EncounterToADT(e)
	if !ok {
		t.Fatal("expected ok for finished status")
	}
	if adt.Type != medical.ADTDischarge {
		t.Errorf("expected ADTDischarge, got %v", adt.Type)
	}
}

func TestEncounterToADT_ArrivedStatus(t *testing.T) {
	e := Encounter{
		Status:  "arrived",
		Subject: Reference{Reference: "Patient/pt-001"},
	}
	adt, ok := EncounterToADT(e)
	if !ok {
		t.Fatal("expected ok for arrived status")
	}
	if adt.Type != medical.ADTAdmit {
		t.Errorf("expected ADTAdmit, got %v", adt.Type)
	}
}

func TestEncounterToADT_WithReasonCode(t *testing.T) {
	e := Encounter{
		Status:     "in-progress",
		Subject:    Reference{Reference: "Patient/pt-001"},
		ReasonCode: []CodeableConcept{{Text: "chest pain"}},
	}
	adt, ok := EncounterToADT(e)
	if !ok {
		t.Fatal("expected ok")
	}
	if adt.PrimaryDx != "chest pain" {
		t.Errorf("expected PrimaryDx=chest pain, got %q", adt.PrimaryDx)
	}
}

// ---------------------------------------------------------------------------
// metricToLOINC — default case
// ---------------------------------------------------------------------------

func TestMetricToLOINC_UnknownMetric(t *testing.T) {
	code, display := metricToLOINC("unknown_metric")
	if code != "unknown" {
		t.Errorf("expected code=unknown, got %q", code)
	}
	if display != "unknown_metric" {
		t.Errorf("expected display=unknown_metric, got %q", display)
	}
}

func TestMetricToLOINC_AllKnown(t *testing.T) {
	tests := []struct {
		metric string
		code   string
	}{
		{"systolic_bp", "8480-6"},
		{"weight_lb", "29463-7"},
		{"spo2", "59408-5"},
		{"heart_rate", "8867-4"},
		{"hba1c", "4548-4"},
		{"glucose", "2339-0"},
	}
	for _, tt := range tests {
		code, _ := metricToLOINC(tt.metric)
		if code != tt.code {
			t.Errorf("metric=%q: expected code %q, got %q", tt.metric, tt.code, code)
		}
	}
}
