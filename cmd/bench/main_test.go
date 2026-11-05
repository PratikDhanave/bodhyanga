package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

// writeJSON marshals v with indentation and writes it under tmp so each
// subtest gets a clean filesystem.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadManifest_MergesManifestOverFile(t *testing.T) {
	tmp := t.TempDir()
	casePath := filepath.Join(tmp, "case_x.json")
	writeJSON(t, casePath, map[string]any{
		"case_id":         "from-file",
		"chief_complaint": "Cough",
		"hpi":             "Adult with productive cough",
		"demographics":    "40M",
		"vitals":          "T 38.0",
		"gold_diagnosis":  "from-file-gold",
		"max_cost_usd":    100,
	})
	manifestPath := filepath.Join(tmp, "manifest.json")
	writeJSON(t, manifestPath, manifestFile{
		Version: 1,
		Cases: []manifestEntry{
			{
				ID:            "manifest-id",
				Path:          "case_x.json",
				GoldDiagnosis: "Manifest-Gold",
				MaxCostUSD:    250,
				Condition:     "cap",
				Tags:          []string{"respiratory", "synthetic"},
			},
		},
	})

	fixtures, err := loadManifest(manifestPath)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(fixtures) != 1 {
		t.Fatalf("expected 1 fixture, got %d", len(fixtures))
	}
	got := fixtures[0]
	if got.CaseID != "manifest-id" {
		t.Errorf("CaseID: want manifest-id, got %q", got.CaseID)
	}
	if got.GoldDiagnosis != "Manifest-Gold" {
		t.Errorf("GoldDiagnosis: want Manifest-Gold, got %q", got.GoldDiagnosis)
	}
	if got.MaxCostUSD != 250 {
		t.Errorf("MaxCostUSD: want 250, got %v", got.MaxCostUSD)
	}
	if got.Condition != "cap" {
		t.Errorf("Condition: want cap, got %q", got.Condition)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "respiratory" {
		t.Errorf("Tags: want [respiratory synthetic], got %v", got.Tags)
	}
	// per-file fields that the manifest didn't override should survive.
	if got.ChiefComplaint != "Cough" {
		t.Errorf("ChiefComplaint should be carried from file, got %q", got.ChiefComplaint)
	}
}

func TestLoadManifest_RejectsEmpty(t *testing.T) {
	tmp := t.TempDir()
	manifestPath := filepath.Join(tmp, "manifest.json")
	writeJSON(t, manifestPath, manifestFile{Version: 1})
	if _, err := loadManifest(manifestPath); err == nil {
		t.Fatal("expected error for empty manifest, got nil")
	}
}

func TestLoadManifest_RejectsMissingPath(t *testing.T) {
	tmp := t.TempDir()
	manifestPath := filepath.Join(tmp, "manifest.json")
	writeJSON(t, manifestPath, manifestFile{
		Version: 1,
		Cases:   []manifestEntry{{ID: "x"}}, // no Path
	})
	if _, err := loadManifest(manifestPath); err == nil {
		t.Fatal("expected error for missing case path, got nil")
	}
}

// TestRepoManifestLoads exercises the real data/cases/manifest.json so a
// malformed manifest or a missing case file is caught by `go test ./...`.
func TestRepoManifestLoads(t *testing.T) {
	root := repoRoot(t)
	manifest := filepath.Join(root, "data/cases/manifest.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Skipf("manifest %s not present: %v", manifest, err)
	}
	fixtures, err := loadManifest(manifest)
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if len(fixtures) < 5 {
		t.Fatalf("expected >= 5 fixtures in repo manifest, got %d", len(fixtures))
	}
	seen := make(map[string]bool, len(fixtures))
	for _, f := range fixtures {
		if f.CaseID == "" {
			t.Errorf("fixture from %s has empty CaseID", f.FilePath)
		}
		if f.GoldDiagnosis == "" {
			t.Errorf("fixture %s has empty GoldDiagnosis", f.CaseID)
		}
		if seen[f.CaseID] {
			t.Errorf("duplicate case_id %s in manifest", f.CaseID)
		}
		seen[f.CaseID] = true
	}
}

// repoRoot walks up from the test's working directory to find the module
// root (the directory containing go.mod). Keeps tests robust to where `go
// test` is invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod walking up from test cwd")
		}
		dir = parent
	}
}

func TestSummarizeProvider_RuleLikeResults(t *testing.T) {
	run := providerRun{
		Provider: "rule",
		Results: []result{
			synthResult("c1", true, 200, true, 1),
			synthResult("c2", true, 220, true, 0),
			synthResult("c3", false, 240, false, 0), // over budget
		},
	}
	s := summarizeProvider(run)
	if s.Cases != 3 {
		t.Fatalf("Cases: want 3, got %d", s.Cases)
	}
	if got := s.AccuracyPct; got < 66 || got > 67 {
		t.Errorf("AccuracyPct: want ~66.67, got %.2f", got)
	}
	if s.MeanCostUSD != 220 {
		t.Errorf("MeanCostUSD: want 220, got %v", s.MeanCostUSD)
	}
	if got := s.UnderBudgetPct; got < 66 || got > 67 {
		t.Errorf("UnderBudgetPct: want ~66.67, got %.2f", got)
	}
	// Pareto = (2/3) / 220 ≈ 0.00303
	if s.Pareto < 0.002 || s.Pareto > 0.004 {
		t.Errorf("Pareto out of expected range, got %f", s.Pareto)
	}
}

func TestDetectRegressions_FlagsAccuracyDrop(t *testing.T) {
	baseline := baselineFile{
		Providers: []baselineEntry{
			{Provider: "rule", AccuracyPct: 80.0, MeanCostUSD: 200.0},
		},
	}
	summaries := []providerSummary{
		{Provider: "rule", AccuracyPct: 60.0, MeanCostUSD: 200.0},
	}
	got := detectRegressions(baseline, summaries)
	if len(got) != 1 {
		t.Fatalf("expected 1 regression, got %d (%v)", len(got), got)
	}
	if !strings.Contains(got[0], "accuracy") {
		t.Errorf("regression text should mention accuracy, got %q", got[0])
	}
}

func TestDetectRegressions_FlagsCostIncrease(t *testing.T) {
	baseline := baselineFile{
		Providers: []baselineEntry{
			{Provider: "llm-openai", AccuracyPct: 80.0, MeanCostUSD: 200.0},
		},
	}
	summaries := []providerSummary{
		{Provider: "llm-openai", AccuracyPct: 80.0, MeanCostUSD: 320.0},
	}
	got := detectRegressions(baseline, summaries)
	if len(got) != 1 {
		t.Fatalf("expected 1 regression, got %d (%v)", len(got), got)
	}
	if !strings.Contains(got[0], "cost") {
		t.Errorf("regression text should mention cost, got %q", got[0])
	}
}

func TestDetectRegressions_ToleratesJitter(t *testing.T) {
	baseline := baselineFile{
		Providers: []baselineEntry{
			{Provider: "rule", AccuracyPct: 80.0, MeanCostUSD: 200.0},
		},
	}
	// 0.5pp drop + 0.5¢ rise should both fall within tolerance.
	summaries := []providerSummary{
		{Provider: "rule", AccuracyPct: 79.5, MeanCostUSD: 200.005},
	}
	if got := detectRegressions(baseline, summaries); len(got) != 0 {
		t.Errorf("expected no regressions inside tolerance, got %v", got)
	}
}

func TestDetectRegressions_IgnoresUnknownProvider(t *testing.T) {
	baseline := baselineFile{
		Providers: []baselineEntry{
			{Provider: "rule", AccuracyPct: 80.0, MeanCostUSD: 200.0},
		},
	}
	// Provider not in baseline: don't fail (no baseline = nothing to regress
	// against). This keeps adding a new provider non-breaking.
	summaries := []providerSummary{
		{Provider: "llm-ollama", AccuracyPct: 10.0, MeanCostUSD: 999.0},
	}
	if got := detectRegressions(baseline, summaries); len(got) != 0 {
		t.Errorf("expected no regressions for unknown provider, got %v", got)
	}
}

func TestFormatHeadlineTable_IncludesAllColumns(t *testing.T) {
	summaries := []providerSummary{
		{
			Provider: "rule", Cases: 5, AccuracyPct: 80, MeanCostUSD: 245,
			MeanSteps: 11.2, UnderBudgetPct: 100, MeanRPMAlerts: 0.4, Pareto: 0.327,
		},
	}
	out := formatHeadlineTable(summaries)
	for _, want := range []string{
		"Provider", "Cases", "Accuracy", "Mean Cost", "Mean Steps",
		"Under Budget", "Pareto", "rule", "80.0%", "$  245.00", "0.327",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formatted table missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestWriteBaseline_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "baseline.json")
	summaries := []providerSummary{
		{Provider: "rule", Cases: 7, AccuracyPct: 42.9, MeanCostUSD: 220.0},
		{Provider: "llm-openai", Cases: 7, AccuracyPct: 71.4, MeanCostUSD: 280.0},
	}
	if err := writeBaseline(path, summaries); err != nil {
		t.Fatalf("writeBaseline: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var bf baselineFile
	if err := json.Unmarshal(b, &bf); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	if len(bf.Providers) != 2 {
		t.Fatalf("expected 2 providers in baseline, got %d", len(bf.Providers))
	}
	if bf.Providers[0].Provider != "rule" || bf.Providers[0].Cases != 7 {
		t.Errorf("unexpected first provider entry: %+v", bf.Providers[0])
	}
	if bf.Providers[0].RecordedAt == "" {
		t.Error("RecordedAt should be populated")
	}
}

func TestLoadCases_PrefersManifest(t *testing.T) {
	tmp := t.TempDir()
	casePath := filepath.Join(tmp, "case_y.json")
	writeJSON(t, casePath, map[string]any{
		"case_id":         "via-manifest",
		"chief_complaint": "Test",
		"gold_diagnosis":  "Gold",
		"max_cost_usd":    100,
	})
	manifestPath := filepath.Join(tmp, "manifest.json")
	writeJSON(t, manifestPath, manifestFile{
		Version: 1,
		Cases: []manifestEntry{
			{ID: "via-manifest", Path: "case_y.json", GoldDiagnosis: "Gold"},
		},
	})
	fixtures, err := loadCases(manifestPath, "")
	if err != nil {
		t.Fatalf("loadCases: %v", err)
	}
	if len(fixtures) != 1 || fixtures[0].CaseID != "via-manifest" {
		t.Fatalf("expected via-manifest fixture, got %+v", fixtures)
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV("rule, llm-openai ,llm-ollama")
	want := []string{"rule", "llm-openai", "llm-ollama"}
	if len(got) != len(want) {
		t.Fatalf("len: want %d, got %d (%v)", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("idx %d: want %q, got %q", i, w, got[i])
		}
	}
}

// synthResult builds a synthetic per-case result without spinning up the bus.
// Useful for exercising summarize/regression logic in isolation.
func synthResult(caseID string, match bool, cost float64, underBudget bool, rpmAlerts int) result {
	acc := 0.0
	if match {
		acc = 1.0
	}
	return result{
		Fixture: fixture{CaseID: caseID},
		State: medical.CaseState{
			WorkingDiagnosis: caseID,
			GoldDiagnosis:    caseID,
			TotalCostUSD:     cost,
		},
		Metrics: eval.CaseMetrics{
			DiagnosisMatch: match,
			Accuracy:       acc,
			CostUSD:        cost,
			UnderBudget:    underBudget,
			RPMAlertCount:  rpmAlerts,
			MessageSteps:   11,
		},
	}
}
