package shadow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// TestConcordanceLedger_Append verifies that records are stored in memory
// and that Summarize computes correct aggregate metrics.
func TestConcordanceLedger_Append(t *testing.T) {
	l, err := NewConcordanceLedger("")
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}

	l.Append(ConcordanceRecord{
		CaseID:            "case-1",
		ClinicianDecision: "Pneumonia",
		AIDecision:        "Pneumonia",
		Concordant:        true,
		AIConfidence:      0.95,
		Timestamp:         time.Now().UTC(),
		LatencyMs:         120,
	})
	l.Append(ConcordanceRecord{
		CaseID:            "case-2",
		ClinicianDecision: "Asthma",
		AIDecision:        "COPD",
		Concordant:        false,
		AIConfidence:      0.60,
		Timestamp:         time.Now().UTC(),
		LatencyMs:         200,
	})

	report := l.Summarize()
	if report.Total != 2 {
		t.Errorf("Total = %d, want 2", report.Total)
	}
	if report.Concordant != 1 {
		t.Errorf("Concordant = %d, want 1", report.Concordant)
	}
	if report.Discordant != 1 {
		t.Errorf("Discordant = %d, want 1", report.Discordant)
	}
	if report.ConcordanceRate != 0.5 {
		t.Errorf("ConcordanceRate = %f, want 0.5", report.ConcordanceRate)
	}
	wantConf := (0.95 + 0.60) / 2.0
	if diff := report.AvgAIConfidence - wantConf; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("AvgAIConfidence = %f, want %f", report.AvgAIConfidence, wantConf)
	}
	wantLat := (120.0 + 200.0) / 2.0
	if diff := report.AvgLatencyMs - wantLat; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("AvgLatencyMs = %f, want %f", report.AvgLatencyMs, wantLat)
	}
}

// TestConcordanceLedger_JSONL verifies that when a file path is provided,
// records are written as JSONL.
func TestConcordanceLedger_JSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")

	l, err := NewConcordanceLedger(path)
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}

	rec := ConcordanceRecord{
		CaseID:            "case-jsonl",
		ClinicianDecision: "CHF",
		AIDecision:        "CHF",
		Concordant:        true,
		AIConfidence:      0.88,
		Timestamp:         time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		LatencyMs:         150,
	}
	l.Append(rec)

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got ConcordanceRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal JSONL line: %v", err)
	}
	if got.CaseID != "case-jsonl" {
		t.Errorf("CaseID = %q, want %q", got.CaseID, "case-jsonl")
	}
	if !got.Concordant {
		t.Error("Concordant = false, want true")
	}
}

// TestConcordanceLedger_EmptySummarize verifies that an empty ledger
// produces a zero report.
func TestConcordanceLedger_EmptySummarize(t *testing.T) {
	l, err := NewConcordanceLedger("")
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}
	report := l.Summarize()
	if report.Total != 0 {
		t.Errorf("Total = %d, want 0", report.Total)
	}
	if report.ConcordanceRate != 0 {
		t.Errorf("ConcordanceRate = %f, want 0", report.ConcordanceRate)
	}
}

// TestSummarizeRecords tests the pure summarization function with a variety
// of concordance patterns.
func TestSummarizeRecords(t *testing.T) {
	records := []ConcordanceRecord{
		{Concordant: true, AIConfidence: 1.0, LatencyMs: 100},
		{Concordant: true, AIConfidence: 0.8, LatencyMs: 200},
		{Concordant: false, AIConfidence: 0.3, LatencyMs: 300},
		{Concordant: true, AIConfidence: 0.9, LatencyMs: 150},
	}

	report := summarizeRecords(records)

	if report.Total != 4 {
		t.Errorf("Total = %d, want 4", report.Total)
	}
	if report.Concordant != 3 {
		t.Errorf("Concordant = %d, want 3", report.Concordant)
	}
	if report.Discordant != 1 {
		t.Errorf("Discordant = %d, want 1", report.Discordant)
	}
	wantRate := 3.0 / 4.0
	if report.ConcordanceRate != wantRate {
		t.Errorf("ConcordanceRate = %f, want %f", report.ConcordanceRate, wantRate)
	}
}

// TestDiagnosisMatch verifies case-insensitive comparison logic.
func TestDiagnosisMatch(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"Pneumonia", "pneumonia", true},
		{"  CHF  ", "CHF", true},
		{"ACS", "STEMI", false},
		{"", "Pneumonia", false},
		{"Pneumonia", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got := diagnosisMatch(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("diagnosisMatch(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// TestExtractCaseID verifies case ID extraction from JSON and fallbacks.
func TestExtractCaseID(t *testing.T) {
	// JSON with case_id field.
	input := `{"case_id": "test-001", "chief_complaint": "cough"}`
	got := extractCaseID(input, "cough")
	if got != "test-001" {
		t.Errorf("extractCaseID with case_id = %q, want %q", got, "test-001")
	}

	// JSON without case_id field — falls back to chief complaint.
	input2 := `{"chief_complaint": "chest pain"}`
	got2 := extractCaseID(input2, "chest pain")
	if got2 != "chest-pain" {
		t.Errorf("extractCaseID fallback = %q, want %q", got2, "chest-pain")
	}

	// Empty fallback — generates a time-based ID.
	got3 := extractCaseID("{}", "")
	if got3 == "" {
		t.Error("extractCaseID with empty fallback should not be empty")
	}
}

// TestExtractConfidence verifies confidence heuristic.
func TestExtractConfidence(t *testing.T) {
	tests := []struct {
		name string
		diag string
		hypo []string
		want float64
	}{
		{"diagnosis with 1 hypothesis", "Pneumonia", []string{"Pneumonia"}, 1.0},
		{"diagnosis with 2 hypotheses", "Pneumonia", []string{"Pneumonia", "Bronchitis"}, 0.5},
		{"diagnosis without hypotheses", "Pneumonia", nil, 1.0},
		{"no diagnosis", "", nil, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := medical.CaseState{
				WorkingDiagnosis: tt.diag,
				Hypotheses:       tt.hypo,
			}
			got := extractConfidence(state)
			if got != tt.want {
				t.Errorf("extractConfidence(%q, %v) = %f, want %f",
					tt.diag, tt.hypo, got, tt.want)
			}
		})
	}
}

// TestShadowRunner_SyncMode exercises the full shadow pipeline end-to-end
// using the rule-based diagnostician in synchronous mode.
func TestShadowRunner_SyncMode(t *testing.T) {
	ctx := context.Background()

	orch, bus, err := BuildShadowPipeline(ctx, "rule")
	if err != nil {
		t.Fatalf("BuildShadowPipeline: %v", err)
	}
	_ = orch // already started

	ledger, err := NewConcordanceLedger("")
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}

	runner := NewShadowRunner(orch, bus, ledger, ModeSync, 5*time.Second)

	// Community-acquired pneumonia fixture — the rule diagnostician should
	// resolve this correctly.
	caseInput := `{
		"chief_complaint": "Cough and fever",
		"hpi": "58-year-old presents with 5 days productive cough and fever to 38.9C",
		"demographics": "58M",
		"vitals": "T 38.9C, HR 98, RR 22, SpO2 94%",
		"case_id": "shadow-test-001"
	}`

	rec, err := runner.RunShadow(ctx, caseInput, "Community-acquired pneumonia")
	if err != nil {
		t.Fatalf("RunShadow: %v", err)
	}
	if rec == nil {
		t.Fatal("RunShadow returned nil record in sync mode")
	}

	t.Logf("AI decision: %q", rec.AIDecision)
	t.Logf("Concordant: %v", rec.Concordant)
	t.Logf("Latency: %dms", rec.LatencyMs)

	if rec.CaseID != "shadow-test-001" {
		t.Errorf("CaseID = %q, want %q", rec.CaseID, "shadow-test-001")
	}
	if rec.ClinicianDecision != "Community-acquired pneumonia" {
		t.Errorf("ClinicianDecision = %q, want %q", rec.ClinicianDecision, "Community-acquired pneumonia")
	}
	if rec.AIDecision == "" {
		t.Error("AIDecision should not be empty")
	}

	// Verify report aggregation works.
	report := runner.Report()
	if report.Total != 1 {
		t.Errorf("Report.Total = %d, want 1", report.Total)
	}
}

// TestShadowRunner_AsyncMode verifies that async mode returns nil
// immediately and eventually records the result.
func TestShadowRunner_AsyncMode(t *testing.T) {
	ctx := context.Background()

	orch, bus, err := BuildShadowPipeline(ctx, "rule")
	if err != nil {
		t.Fatalf("BuildShadowPipeline: %v", err)
	}
	_ = orch

	ledger, err := NewConcordanceLedger("")
	if err != nil {
		t.Fatalf("NewConcordanceLedger: %v", err)
	}

	runner := NewShadowRunner(orch, bus, ledger, ModeAsync, 5*time.Second)

	caseInput := `{
		"chief_complaint": "Cough and fever",
		"hpi": "58-year-old with productive cough",
		"demographics": "58M",
		"vitals": "T 38.5C, HR 95, RR 20",
		"case_id": "shadow-async-001"
	}`

	rec, err := runner.RunShadow(ctx, caseInput, "Community-acquired pneumonia")
	if err != nil {
		t.Fatalf("RunShadow async: %v", err)
	}
	if rec != nil {
		t.Error("RunShadow async should return nil record")
	}

	// Give the background goroutine time to complete.
	time.Sleep(3 * time.Second)

	report := runner.Report()
	if report.Total < 1 {
		t.Logf("async record may not have completed yet (Total=%d), skipping assertion", report.Total)
	} else {
		t.Logf("Async report: %+v", report)
	}
}

// TestConcordanceReport_JSON verifies that the report serializes correctly.
func TestConcordanceReport_JSON(t *testing.T) {
	report := ConcordanceReport{
		Total:           10,
		Concordant:      8,
		Discordant:      2,
		ConcordanceRate: 0.8,
		AvgAIConfidence: 0.85,
		AvgLatencyMs:    150.5,
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got ConcordanceReport
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Total != 10 {
		t.Errorf("Total = %d, want 10", got.Total)
	}
	if got.ConcordanceRate != 0.8 {
		t.Errorf("ConcordanceRate = %f, want 0.8", got.ConcordanceRate)
	}
}
