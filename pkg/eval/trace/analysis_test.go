package trace

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ptr(s string) *string { return &s }

func sampleTraces() []Trace {
	return []Trace{
		{
			ID:    "t1",
			Query: "patient with chest pain and elevated troponin",
			Spans: []Span{
				{Name: "llm_call", Input: "classify", Output: "cardiology", Duration: 200 * time.Millisecond},
				{Name: "tool_call", Input: "lookup labs", Output: "troponin=2.1", Duration: 50 * time.Millisecond},
				{Name: "llm_call", Input: "diagnose", Output: "STEMI", Duration: 300 * time.Millisecond},
			},
			FinalOutput:  "ST-elevation myocardial infarction",
			Metadata:     map[string]any{"persona": "cardiologist", "severity": "high"},
			Duration:     600 * time.Millisecond,
			Timestamp:    time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			UserFeedback: ptr("thumbs_up"),
		},
		{
			ID:    "t2",
			Query: "cough and fever for three days",
			Spans: []Span{
				{Name: "llm_call", Input: "classify", Output: "pulmonology", Duration: 150 * time.Millisecond},
				{Name: "retrieval", Input: "pneumonia guidelines", Output: "CAP criteria", Duration: 30 * time.Millisecond},
				{Name: "llm_call", Input: "diagnose", Output: "CAP", Duration: 250 * time.Millisecond},
			},
			FinalOutput:  "Community-acquired pneumonia",
			Metadata:     map[string]any{"persona": "internist", "severity": "moderate"},
			Duration:     450 * time.Millisecond,
			Timestamp:    time.Date(2025, 1, 15, 11, 0, 0, 0, time.UTC),
			UserFeedback: ptr("thumbs_down"),
		},
		{
			ID:    "t3",
			Query: "abdominal pain with nausea",
			Spans: []Span{
				{Name: "llm_call", Input: "classify", Output: "gastro", Duration: 180 * time.Millisecond},
				{Name: "tool_call", Input: "imaging", Output: "CT normal", Duration: 100 * time.Millisecond},
			},
			FinalOutput: "Non-specific abdominal pain",
			Metadata:    map[string]any{"persona": "internist", "severity": "low"},
			Duration:    300 * time.Millisecond,
			Timestamp:   time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
			// No feedback
		},
		{
			ID:    "t4",
			Query: "headache and vision changes",
			Spans: []Span{
				{Name: "llm_call", Input: "triage", Output: "neuro urgent", Duration: 100 * time.Millisecond},
				{Name: "tool_call", Input: "eye exam", Output: "papilledema", Duration: 80 * time.Millisecond},
				{Name: "tool_call", Input: "MRI order", Output: "mass found", Duration: 150 * time.Millisecond},
				{Name: "llm_call", Input: "diagnose", Output: "IIH vs mass", Duration: 400 * time.Millisecond},
			},
			FinalOutput:  "Idiopathic intracranial hypertension",
			Metadata:     map[string]any{"persona": "neurologist", "severity": "high"},
			Duration:     800 * time.Millisecond,
			Timestamp:    time.Date(2025, 1, 15, 13, 0, 0, 0, time.UTC),
			UserFeedback: ptr("thumbs_up"),
		},
		{
			ID:    "t5",
			Query: "shortness of breath after surgery",
			Spans: []Span{
				{Name: "llm_call", Input: "assess", Output: "PE risk", Duration: 120 * time.Millisecond},
				{Name: "tool_call", Input: "d-dimer", Output: "elevated", Duration: 60 * time.Millisecond},
				{Name: "tool_call", Input: "CTA", Output: "PE confirmed", Duration: 200 * time.Millisecond},
				{Name: "llm_call", Input: "diagnose", Output: "PE", Duration: 300 * time.Millisecond},
				{Name: "tool_call", Input: "heparin order", Output: "started", Duration: 40 * time.Millisecond, Error: "pharmacy timeout"},
			},
			FinalOutput:  "Pulmonary embolism",
			Metadata:     map[string]any{"persona": "cardiologist", "severity": "high"},
			Duration:     750 * time.Millisecond,
			Timestamp:    time.Date(2025, 1, 15, 14, 0, 0, 0, time.UTC),
			UserFeedback: ptr("thumbs_up"),
		},
	}
}

// ---------------------------------------------------------------------------
// LoadTraces
// ---------------------------------------------------------------------------

func TestLoadTraces_ValidJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	enc := json.NewEncoder(f)
	for _, tr := range sampleTraces() {
		if err := enc.Encode(tr); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	f.Close()

	got, err := LoadTraces(path)
	if err != nil {
		t.Fatalf("LoadTraces: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 traces, got %d", len(got))
	}
	if got[0].ID != "t1" {
		t.Fatalf("first trace ID: got %q, want %q", got[0].ID, "t1")
	}
}

func TestLoadTraces_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTraces(path)
	if err != nil {
		t.Fatalf("LoadTraces empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 traces, got %d", len(got))
	}
}

func TestLoadTraces_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(path, []byte("{not valid json}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadTraces(path)
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse line 1") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestLoadTraces_FileNotFound(t *testing.T) {
	_, err := LoadTraces("/nonexistent/path/traces.jsonl")
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestLoadTraces_SkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "with_blanks.jsonl")
	data, _ := json.Marshal(sampleTraces()[0])
	content := string(data) + "\n\n" + string(data) + "\n  \n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTraces(path)
	if err != nil {
		t.Fatalf("LoadTraces: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 traces (blank lines skipped), got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// ComputeStats
// ---------------------------------------------------------------------------

func TestComputeStats_AllFields(t *testing.T) {
	traces := sampleTraces()
	stats := ComputeStats(traces)

	if stats.TotalTraces != 5 {
		t.Fatalf("TotalTraces: got %d, want 5", stats.TotalTraces)
	}

	// Avg duration: (600+450+300+800+750)/5 = 580ms
	wantAvg := 580 * time.Millisecond
	if stats.AvgDuration != wantAvg {
		t.Fatalf("AvgDuration: got %v, want %v", stats.AvgDuration, wantAvg)
	}

	// Sorted durations: 300, 450, 600, 750, 800
	// P50 = index ceil(0.50*5)-1 = 2 => 600ms
	if stats.P50Duration != 600*time.Millisecond {
		t.Fatalf("P50Duration: got %v, want 600ms", stats.P50Duration)
	}

	// Total spans: 3+3+2+4+5 = 17, avg = 3.4
	if stats.AvgSpans < 3.3 || stats.AvgSpans > 3.5 {
		t.Fatalf("AvgSpans: got %.2f, want ~3.4", stats.AvgSpans)
	}

	// Tool calls: t1=1, t2=0, t3=1, t4=2, t5=3 => total=7, avg=1.4
	if stats.AvgToolCalls < 1.3 || stats.AvgToolCalls > 1.5 {
		t.Fatalf("AvgToolCalls: got %.2f, want ~1.4", stats.AvgToolCalls)
	}

	// Feedback: 4 out of 5 have feedback => 0.8
	if stats.FeedbackRate < 0.79 || stats.FeedbackRate > 0.81 {
		t.Fatalf("FeedbackRate: got %.2f, want 0.80", stats.FeedbackRate)
	}

	// ThumbsUp: 3 out of 4 with feedback => 0.75
	if stats.ThumbsUpRate < 0.74 || stats.ThumbsUpRate > 0.76 {
		t.Fatalf("ThumbsUpRate: got %.2f, want 0.75", stats.ThumbsUpRate)
	}

	// ByMetadata
	if stats.ByMetadata["persona"]["internist"] != 2 {
		t.Fatalf("ByMetadata persona/internist: got %d, want 2", stats.ByMetadata["persona"]["internist"])
	}
	if stats.ByMetadata["severity"]["high"] != 3 {
		t.Fatalf("ByMetadata severity/high: got %d, want 3", stats.ByMetadata["severity"]["high"])
	}
}

func TestComputeStats_Empty(t *testing.T) {
	stats := ComputeStats(nil)
	if stats.TotalTraces != 0 {
		t.Fatalf("expected 0 traces, got %d", stats.TotalTraces)
	}
	if stats.ByMetadata == nil {
		t.Fatalf("ByMetadata should be initialized, not nil")
	}
}

func TestComputeStats_SingleTrace(t *testing.T) {
	traces := sampleTraces()[:1]
	stats := ComputeStats(traces)
	if stats.TotalTraces != 1 {
		t.Fatalf("TotalTraces: %d", stats.TotalTraces)
	}
	if stats.AvgDuration != 600*time.Millisecond {
		t.Fatalf("AvgDuration: %v", stats.AvgDuration)
	}
	if stats.P50Duration != 600*time.Millisecond {
		t.Fatalf("P50: %v", stats.P50Duration)
	}
	if stats.P99Duration != 600*time.Millisecond {
		t.Fatalf("P99: %v", stats.P99Duration)
	}
}

func TestComputeStats_NoFeedback(t *testing.T) {
	traces := []Trace{
		{ID: "a", Duration: 100 * time.Millisecond, Metadata: map[string]any{}},
		{ID: "b", Duration: 200 * time.Millisecond, Metadata: map[string]any{}},
	}
	stats := ComputeStats(traces)
	if stats.FeedbackRate != 0 {
		t.Fatalf("FeedbackRate: got %f, want 0", stats.FeedbackRate)
	}
	if stats.ThumbsUpRate != 0 {
		t.Fatalf("ThumbsUpRate: got %f, want 0", stats.ThumbsUpRate)
	}
}

// ---------------------------------------------------------------------------
// FindOutliers
// ---------------------------------------------------------------------------

func TestFindOutliers_IQR(t *testing.T) {
	// Create traces with one extreme outlier.
	traces := make([]Trace, 20)
	for i := range traces {
		traces[i] = Trace{
			ID:       fmt.Sprintf("t%d", i),
			Duration: 500 * time.Millisecond,
			Metadata: map[string]any{},
		}
	}
	// Make one trace dramatically slower.
	traces[19].Duration = 10 * time.Second

	outliers := FindOutliers(traces, "duration", "iqr")
	if len(outliers) == 0 {
		t.Fatalf("expected at least one outlier")
	}
	found := false
	for _, o := range outliers {
		if o.TraceID == "t19" && o.Direction == "above" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected t19 to be flagged as above outlier, got %+v", outliers)
	}
}

func TestFindOutliers_Percentile(t *testing.T) {
	traces := make([]Trace, 200)
	for i := range traces {
		traces[i] = Trace{
			ID:       fmt.Sprintf("t%d", i),
			Duration: time.Duration(500+i) * time.Millisecond,
			Metadata: map[string]any{},
		}
	}
	// Make the last one extreme.
	traces[199].Duration = 100 * time.Second

	outliers := FindOutliers(traces, "duration", "percentile")
	if len(outliers) == 0 {
		t.Fatalf("expected at least one outlier with percentile method")
	}
	// The extreme value should be flagged.
	found := false
	for _, o := range outliers {
		if o.TraceID == "t199" {
			found = true
			if o.Direction != "above" {
				t.Fatalf("expected direction=above, got %s", o.Direction)
			}
		}
	}
	if !found {
		t.Fatalf("expected t199 in outliers")
	}
}

func TestFindOutliers_Empty(t *testing.T) {
	outliers := FindOutliers(nil, "duration", "iqr")
	if len(outliers) != 0 {
		t.Fatalf("expected nil/empty for nil traces")
	}
}

func TestFindOutliers_UnknownMethod(t *testing.T) {
	traces := sampleTraces()
	outliers := FindOutliers(traces, "duration", "zscore")
	if len(outliers) != 0 {
		t.Fatalf("expected nil for unknown method, got %d", len(outliers))
	}
}

func TestFindOutliers_UnknownMetric(t *testing.T) {
	traces := sampleTraces()
	outliers := FindOutliers(traces, "unknown_metric", "iqr")
	if len(outliers) != 0 {
		t.Fatalf("expected nil for unknown metric, got %d", len(outliers))
	}
}

func TestFindOutliers_SpansMetric(t *testing.T) {
	traces := make([]Trace, 20)
	for i := range traces {
		traces[i] = Trace{
			ID:       fmt.Sprintf("t%d", i),
			Spans:    make([]Span, 3),
			Metadata: map[string]any{},
		}
	}
	// Give one trace a huge number of spans.
	traces[0].Spans = make([]Span, 100)

	outliers := FindOutliers(traces, "spans", "iqr")
	if len(outliers) == 0 {
		t.Fatalf("expected span-count outlier")
	}
}

func TestFindOutliers_OutputLengthMetric(t *testing.T) {
	traces := make([]Trace, 20)
	for i := range traces {
		traces[i] = Trace{
			ID:          fmt.Sprintf("t%d", i),
			FinalOutput: "short",
			Metadata:    map[string]any{},
		}
	}
	traces[0].FinalOutput = strings.Repeat("x", 10000)

	outliers := FindOutliers(traces, "output_length", "iqr")
	if len(outliers) == 0 {
		t.Fatalf("expected output_length outlier")
	}
}

// ---------------------------------------------------------------------------
// CrossTab
// ---------------------------------------------------------------------------

func TestCrossTab_PersonaDuration(t *testing.T) {
	traces := sampleTraces()
	result := CrossTab(traces, "persona", "duration")

	// cardiologist: t1(0.6s) + t5(0.75s) => avg 0.675
	if v, ok := result["cardiologist"]; !ok {
		t.Fatalf("missing cardiologist in cross-tab")
	} else if v < 0.67 || v > 0.68 {
		t.Fatalf("cardiologist avg duration: got %f, want ~0.675", v)
	}

	// internist: t2(0.45s) + t3(0.3s) => avg 0.375
	if v, ok := result["internist"]; !ok {
		t.Fatalf("missing internist in cross-tab")
	} else if v < 0.37 || v > 0.38 {
		t.Fatalf("internist avg duration: got %f, want ~0.375", v)
	}
}

func TestCrossTab_SeveritySpans(t *testing.T) {
	traces := sampleTraces()
	result := CrossTab(traces, "severity", "spans")

	// high: t1(3) + t4(4) + t5(5) => avg 4.0
	if v, ok := result["high"]; !ok {
		t.Fatalf("missing high in cross-tab")
	} else if v != 4.0 {
		t.Fatalf("high avg spans: got %f, want 4.0", v)
	}
}

func TestCrossTab_MissingKey(t *testing.T) {
	traces := sampleTraces()
	result := CrossTab(traces, "nonexistent", "duration")
	if len(result) != 0 {
		t.Fatalf("expected empty result for missing key, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// FilterTraces
// ---------------------------------------------------------------------------

func TestFilterTraces_ByFeedback(t *testing.T) {
	traces := sampleTraces()
	withFeedback := FilterTraces(traces, func(tr Trace) bool {
		return tr.UserFeedback != nil
	})
	if len(withFeedback) != 4 {
		t.Fatalf("expected 4 traces with feedback, got %d", len(withFeedback))
	}
}

func TestFilterTraces_ByDuration(t *testing.T) {
	traces := sampleTraces()
	slow := FilterTraces(traces, func(tr Trace) bool {
		return tr.Duration > 500*time.Millisecond
	})
	// t1(600ms), t4(800ms), t5(750ms)
	if len(slow) != 3 {
		t.Fatalf("expected 3 slow traces, got %d", len(slow))
	}
}

func TestFilterTraces_NoMatch(t *testing.T) {
	traces := sampleTraces()
	result := FilterTraces(traces, func(tr Trace) bool {
		return tr.Duration > 10*time.Second
	})
	if len(result) != 0 {
		t.Fatalf("expected 0, got %d", len(result))
	}
}

func TestFilterTraces_ByError(t *testing.T) {
	traces := sampleTraces()
	withErrors := FilterTraces(traces, func(tr Trace) bool {
		for _, s := range tr.Spans {
			if s.Error != "" {
				return true
			}
		}
		return false
	})
	// Only t5 has an error span
	if len(withErrors) != 1 {
		t.Fatalf("expected 1 trace with errors, got %d", len(withErrors))
	}
	if withErrors[0].ID != "t5" {
		t.Fatalf("expected t5, got %s", withErrors[0].ID)
	}
}

// ---------------------------------------------------------------------------
// SearchTraces
// ---------------------------------------------------------------------------

func TestSearchTraces_QueryField(t *testing.T) {
	traces := sampleTraces()
	results := SearchTraces(traces, "chest pain troponin", "query", 2)
	if len(results) == 0 {
		t.Fatalf("expected at least one result")
	}
	if results[0].ID != "t1" {
		t.Fatalf("expected t1 as top result for chest pain, got %s", results[0].ID)
	}
}

func TestSearchTraces_OutputField(t *testing.T) {
	traces := sampleTraces()
	results := SearchTraces(traces, "pneumonia community acquired", "final_output", 1)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "t2" {
		t.Fatalf("expected t2 for pneumonia search, got %s", results[0].ID)
	}
}

func TestSearchTraces_TopK(t *testing.T) {
	traces := sampleTraces()
	results := SearchTraces(traces, "pain", "query", 3)
	if len(results) > 3 {
		t.Fatalf("expected at most 3 results, got %d", len(results))
	}
}

func TestSearchTraces_NoMatch(t *testing.T) {
	traces := sampleTraces()
	results := SearchTraces(traces, "xyzzyplugh", "query", 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for nonsense query, got %d", len(results))
	}
}

func TestSearchTraces_Empty(t *testing.T) {
	results := SearchTraces(nil, "anything", "query", 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for nil traces")
	}
}

func TestSearchTraces_EmptyQuery(t *testing.T) {
	traces := sampleTraces()
	results := SearchTraces(traces, "", "query", 5)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty query, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// ExportForReview
// ---------------------------------------------------------------------------

func TestExportForReview_CSV(t *testing.T) {
	traces := sampleTraces()
	dir := t.TempDir()
	path := filepath.Join(dir, "review.csv")
	columns := []string{"id", "query", "duration", "feedback"}

	if err := ExportForReview(traces, path, columns); err != nil {
		t.Fatalf("ExportForReview CSV: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open exported csv: %v", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}

	// Header + 5 data rows
	if len(records) != 6 {
		t.Fatalf("expected 6 rows (header+5), got %d", len(records))
	}
	// Check header
	for i, col := range columns {
		if records[0][i] != col {
			t.Fatalf("header[%d]: got %q, want %q", i, records[0][i], col)
		}
	}
	// Check first data row
	if records[1][0] != "t1" {
		t.Fatalf("first row id: got %q, want t1", records[1][0])
	}
}

func TestExportForReview_JSONL(t *testing.T) {
	traces := sampleTraces()[:2]
	dir := t.TempDir()
	path := filepath.Join(dir, "review.jsonl")
	columns := []string{"id", "query", "spans", "error"}

	if err := ExportForReview(traces, path, columns); err != nil {
		t.Fatalf("ExportForReview JSONL: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read exported jsonl: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var row map[string]string
	if err := json.Unmarshal([]byte(lines[0]), &row); err != nil {
		t.Fatalf("parse first jsonl row: %v", err)
	}
	if row["id"] != "t1" {
		t.Fatalf("first row id: got %q, want t1", row["id"])
	}
	if row["spans"] != "3" {
		t.Fatalf("first row spans: got %q, want 3", row["spans"])
	}
}

func TestExportForReview_ErrorColumn(t *testing.T) {
	traces := sampleTraces()[4:5] // t5 has error
	dir := t.TempDir()
	path := filepath.Join(dir, "errors.csv")
	columns := []string{"id", "error"}

	if err := ExportForReview(traces, path, columns); err != nil {
		t.Fatalf("ExportForReview: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if records[1][1] != "pharmacy timeout" {
		t.Fatalf("error column: got %q, want %q", records[1][1], "pharmacy timeout")
	}
}

func TestExportForReview_ToolCallsColumn(t *testing.T) {
	traces := sampleTraces()[:1]
	dir := t.TempDir()
	path := filepath.Join(dir, "tc.csv")
	columns := []string{"id", "tool_calls"}

	if err := ExportForReview(traces, path, columns); err != nil {
		t.Fatalf("ExportForReview: %v", err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	r := csv.NewReader(f)
	records, _ := r.ReadAll()
	// t1 has 1 tool_call span
	if records[1][1] != "1" {
		t.Fatalf("tool_calls: got %q, want 1", records[1][1])
	}
}

// ---------------------------------------------------------------------------
// GenerateReport
// ---------------------------------------------------------------------------

func TestGenerateReport_ContainsKeyInfo(t *testing.T) {
	traces := sampleTraces()
	report := GenerateReport(traces)

	checks := []string{
		"# Trace Analysis Report",
		"Total traces:** 5",
		"Avg duration:",
		"P50 duration:",
		"P95 duration:",
		"P99 duration:",
		"Avg spans per trace:",
		"Avg tool calls per trace:",
		"Feedback rate:",
		"Thumbs-up rate:",
		"Metadata Breakdown",
	}
	for _, want := range checks {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q", want)
		}
	}
}

func TestGenerateReport_Empty(t *testing.T) {
	report := GenerateReport(nil)
	if !strings.Contains(report, "Total traces:** 0") {
		t.Fatalf("expected 0 traces in empty report, got:\n%s", report)
	}
}

// ---------------------------------------------------------------------------
// Roundtrip: load -> analyze -> export
// ---------------------------------------------------------------------------

func TestRoundtrip_LoadAnalyzeExport(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.jsonl")
	outputPath := filepath.Join(dir, "output.csv")

	// Write traces.
	f, _ := os.Create(inputPath)
	enc := json.NewEncoder(f)
	for _, tr := range sampleTraces() {
		enc.Encode(tr)
	}
	f.Close()

	// Load.
	traces, err := LoadTraces(inputPath)
	if err != nil {
		t.Fatalf("LoadTraces: %v", err)
	}

	// Filter to slow traces.
	slow := FilterTraces(traces, func(tr Trace) bool {
		return tr.Duration > 500*time.Millisecond
	})
	if len(slow) != 3 {
		t.Fatalf("expected 3 slow traces, got %d", len(slow))
	}

	// Compute stats on filtered set.
	stats := ComputeStats(slow)
	if stats.TotalTraces != 3 {
		t.Fatalf("stats on filtered: %d", stats.TotalTraces)
	}

	// Export filtered for review.
	if err := ExportForReview(slow, outputPath, []string{"id", "query", "duration"}); err != nil {
		t.Fatalf("ExportForReview: %v", err)
	}

	// Verify export.
	data, _ := os.ReadFile(outputPath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// 1 header + 3 data
	if len(lines) != 4 {
		t.Fatalf("expected 4 CSV lines, got %d", len(lines))
	}
}
