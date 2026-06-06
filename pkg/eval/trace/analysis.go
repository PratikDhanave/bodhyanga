package trace

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// LoadTraces reads a JSONL file where each line is a JSON-encoded Trace.
// It returns an error if the file cannot be opened or any line fails to parse.
func LoadTraces(path string) ([]Trace, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("trace: open %s: %w", path, err)
	}
	defer f.Close()

	var traces []Trace
	scanner := bufio.NewScanner(f)
	// Allow up to 10 MB per line for large trace payloads.
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var t Trace
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return nil, fmt.Errorf("trace: parse line %d: %w", lineNum, err)
		}
		traces = append(traces, t)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("trace: scan %s: %w", path, err)
	}
	return traces, nil
}

// ---------------------------------------------------------------------------
// Aggregate stats
// ---------------------------------------------------------------------------

// ComputeStats produces aggregate statistics over a slice of traces.
func ComputeStats(traces []Trace) TraceStats {
	n := len(traces)
	if n == 0 {
		return TraceStats{ByMetadata: make(map[string]map[string]int)}
	}

	durations := make([]time.Duration, n)
	var totalDur time.Duration
	var totalSpans, totalTools int
	var withFeedback, thumbsUp int

	byMeta := make(map[string]map[string]int)

	for i, tr := range traces {
		durations[i] = tr.Duration
		totalDur += tr.Duration
		totalSpans += len(tr.Spans)
		totalTools += countToolCalls(tr)

		if tr.UserFeedback != nil {
			withFeedback++
			if *tr.UserFeedback == "thumbs_up" {
				thumbsUp++
			}
		}

		for k, v := range tr.Metadata {
			vs := fmt.Sprintf("%v", v)
			if byMeta[k] == nil {
				byMeta[k] = make(map[string]int)
			}
			byMeta[k][vs]++
		}
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	feedbackRate := 0.0
	if n > 0 {
		feedbackRate = float64(withFeedback) / float64(n)
	}
	thumbsUpRate := 0.0
	if withFeedback > 0 {
		thumbsUpRate = float64(thumbsUp) / float64(withFeedback)
	}

	return TraceStats{
		TotalTraces:  n,
		AvgDuration:  totalDur / time.Duration(n),
		P50Duration:  percentileDuration(durations, 50),
		P95Duration:  percentileDuration(durations, 95),
		P99Duration:  percentileDuration(durations, 99),
		AvgSpans:     float64(totalSpans) / float64(n),
		AvgToolCalls: float64(totalTools) / float64(n),
		FeedbackRate: feedbackRate,
		ThumbsUpRate: thumbsUpRate,
		ByMetadata:   byMeta,
	}
}

func countToolCalls(tr Trace) int {
	count := 0
	for _, s := range tr.Spans {
		if s.Name == "tool_call" {
			count++
		}
	}
	return count
}

func percentileDuration(sorted []time.Duration, pct float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(pct/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ---------------------------------------------------------------------------
// Outlier detection
// ---------------------------------------------------------------------------

// FindOutliers detects traces whose metric value falls outside normal bounds.
//
// Supported metrics: "duration", "spans", "tool_calls", "output_length".
// Supported methods: "iqr" (1.5 x IQR fences) and "percentile" (top/bottom 1%).
func FindOutliers(traces []Trace, metric string, method string) []OutlierReport {
	if len(traces) == 0 {
		return nil
	}

	values := extractMetric(traces, metric)
	if len(values) == 0 {
		return nil
	}

	var reports []OutlierReport

	switch method {
	case "iqr":
		reports = outlierIQR(traces, values, metric)
	case "percentile":
		reports = outlierPercentile(traces, values, metric)
	default:
		return nil
	}
	return reports
}

func extractMetric(traces []Trace, metric string) []float64 {
	vals := make([]float64, len(traces))
	for i, tr := range traces {
		switch metric {
		case "duration":
			vals[i] = tr.Duration.Seconds()
		case "spans":
			vals[i] = float64(len(tr.Spans))
		case "tool_calls":
			vals[i] = float64(countToolCalls(tr))
		case "output_length":
			vals[i] = float64(len(tr.FinalOutput))
		default:
			return nil
		}
	}
	return vals
}

func outlierIQR(traces []Trace, values []float64, metric string) []OutlierReport {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	q1 := percentileFloat(sorted, 25)
	q3 := percentileFloat(sorted, 75)
	iqr := q3 - q1
	lower := q1 - 1.5*iqr
	upper := q3 + 1.5*iqr

	var reports []OutlierReport
	for i, v := range values {
		if v > upper {
			reports = append(reports, OutlierReport{
				TraceID:   traces[i].ID,
				Metric:    metric,
				Value:     v,
				Threshold: upper,
				Direction: "above",
			})
		} else if v < lower {
			reports = append(reports, OutlierReport{
				TraceID:   traces[i].ID,
				Metric:    metric,
				Value:     v,
				Threshold: lower,
				Direction: "below",
			})
		}
	}
	return reports
}

func outlierPercentile(traces []Trace, values []float64, metric string) []OutlierReport {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	low := percentileFloat(sorted, 1)
	high := percentileFloat(sorted, 99)

	var reports []OutlierReport
	for i, v := range values {
		if v > high {
			reports = append(reports, OutlierReport{
				TraceID:   traces[i].ID,
				Metric:    metric,
				Value:     v,
				Threshold: high,
				Direction: "above",
			})
		} else if v < low {
			reports = append(reports, OutlierReport{
				TraceID:   traces[i].ID,
				Metric:    metric,
				Value:     v,
				Threshold: low,
				Direction: "below",
			})
		}
	}
	return reports
}

func percentileFloat(sorted []float64, pct float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(pct/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// ---------------------------------------------------------------------------
// Cross-tabulation
// ---------------------------------------------------------------------------

// CrossTab groups traces by a metadata key and computes the average of the
// named metric for each group.
//
// Example: CrossTab(traces, "persona", "duration") returns something like
// {"investor": 1.2, "buyer": 0.8} where values are in seconds for the
// "duration" metric.
func CrossTab(traces []Trace, metadataKey string, metric string) map[string]float64 {
	groups := make(map[string][]float64)
	for _, tr := range traces {
		v, ok := tr.Metadata[metadataKey]
		if !ok {
			continue
		}
		key := fmt.Sprintf("%v", v)
		vals := extractMetricSingle(tr, metric)
		groups[key] = append(groups[key], vals)
	}

	result := make(map[string]float64, len(groups))
	for k, vals := range groups {
		sum := 0.0
		for _, v := range vals {
			sum += v
		}
		result[k] = sum / float64(len(vals))
	}
	return result
}

func extractMetricSingle(tr Trace, metric string) float64 {
	switch metric {
	case "duration":
		return tr.Duration.Seconds()
	case "spans":
		return float64(len(tr.Spans))
	case "tool_calls":
		return float64(countToolCalls(tr))
	case "output_length":
		return float64(len(tr.FinalOutput))
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

// FilterTraces returns the subset of traces for which predicate returns true.
func FilterTraces(traces []Trace, predicate func(Trace) bool) []Trace {
	var out []Trace
	for _, tr := range traces {
		if predicate(tr) {
			out = append(out, tr)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Semantic search (basic word-overlap / TF-IDF-like scoring)
// ---------------------------------------------------------------------------

// SearchTraces ranks traces by word-overlap similarity to query and returns
// the top-K results. The field parameter selects which Trace field to compare
// against: "query" or "final_output".
//
// This uses a simple TF-IDF-like scoring approach: the overlap of lowercased
// query words present in the target field, weighted by inverse document
// frequency across the corpus. This avoids importing heavyweight embedding
// dependencies while still being useful for basic exploration.
func SearchTraces(traces []Trace, query string, field string, topK int) []Trace {
	if len(traces) == 0 || topK <= 0 {
		return nil
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	// Build document frequency for IDF.
	df := make(map[string]int)
	for _, tr := range traces {
		text := fieldText(tr, field)
		seen := make(map[string]bool)
		for _, w := range tokenize(text) {
			if !seen[w] {
				df[w]++
				seen[w] = true
			}
		}
	}

	type scored struct {
		idx   int
		score float64
	}

	n := float64(len(traces))
	var results []scored
	for i, tr := range traces {
		text := fieldText(tr, field)
		docTerms := tokenize(text)
		tf := make(map[string]int)
		for _, w := range docTerms {
			tf[w]++
		}

		score := 0.0
		for _, qt := range queryTerms {
			if cnt, ok := tf[qt]; ok {
				idf := math.Log(1 + n/float64(df[qt]+1))
				score += float64(cnt) * idf
			}
		}
		if score > 0 {
			results = append(results, scored{idx: i, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })

	if topK > len(results) {
		topK = len(results)
	}
	out := make([]Trace, topK)
	for i := 0; i < topK; i++ {
		out[i] = traces[results[i].idx]
	}
	return out
}

func fieldText(tr Trace, field string) string {
	switch field {
	case "query":
		return tr.Query
	case "final_output":
		return tr.FinalOutput
	default:
		return tr.Query
	}
}

func tokenize(text string) []string {
	text = strings.ToLower(text)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
	// Remove very short tokens (likely noise).
	var out []string
	for _, w := range words {
		if len(w) >= 2 {
			out = append(out, w)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Export for review
// ---------------------------------------------------------------------------

// ExportForReview writes traces to a file in CSV or JSONL format. The format
// is determined by the file extension of path (".csv" for CSV, anything else
// for JSONL). The columns parameter selects which fields to include; supported
// values: "id", "query", "final_output", "duration", "spans", "tool_calls",
// "feedback", "error".
func ExportForReview(traces []Trace, path string, columns []string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".csv" {
		return exportCSV(traces, path, columns)
	}
	return exportJSONL(traces, path, columns)
}

func exportCSV(traces []Trace, path string, columns []string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("trace: create %s: %w", path, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write(columns); err != nil {
		return fmt.Errorf("trace: write csv header: %w", err)
	}

	for _, tr := range traces {
		row := make([]string, len(columns))
		for j, col := range columns {
			row[j] = columnValue(tr, col)
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("trace: write csv row: %w", err)
		}
	}
	return nil
}

func exportJSONL(traces []Trace, path string, columns []string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("trace: create %s: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, tr := range traces {
		row := make(map[string]string, len(columns))
		for _, col := range columns {
			row[col] = columnValue(tr, col)
		}
		if err := enc.Encode(row); err != nil {
			return fmt.Errorf("trace: encode jsonl row: %w", err)
		}
	}
	return nil
}

func columnValue(tr Trace, col string) string {
	switch col {
	case "id":
		return tr.ID
	case "query":
		return tr.Query
	case "final_output":
		return tr.FinalOutput
	case "duration":
		return tr.Duration.String()
	case "spans":
		return fmt.Sprintf("%d", len(tr.Spans))
	case "tool_calls":
		return fmt.Sprintf("%d", countToolCalls(tr))
	case "feedback":
		if tr.UserFeedback != nil {
			return *tr.UserFeedback
		}
		return ""
	case "error":
		for _, s := range tr.Spans {
			if s.Error != "" {
				return s.Error
			}
		}
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Report generation
// ---------------------------------------------------------------------------

// GenerateReport produces a human-readable Markdown summary of a trace
// corpus, including aggregate stats, outliers, and metadata breakdowns.
func GenerateReport(traces []Trace) string {
	stats := ComputeStats(traces)
	outliers := FindOutliers(traces, "duration", "iqr")

	var b strings.Builder
	b.WriteString("# Trace Analysis Report\n\n")

	// Summary
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- **Total traces:** %d\n", stats.TotalTraces)
	fmt.Fprintf(&b, "- **Avg duration:** %s\n", stats.AvgDuration)
	fmt.Fprintf(&b, "- **P50 duration:** %s\n", stats.P50Duration)
	fmt.Fprintf(&b, "- **P95 duration:** %s\n", stats.P95Duration)
	fmt.Fprintf(&b, "- **P99 duration:** %s\n", stats.P99Duration)
	fmt.Fprintf(&b, "- **Avg spans per trace:** %.1f\n", stats.AvgSpans)
	fmt.Fprintf(&b, "- **Avg tool calls per trace:** %.1f\n", stats.AvgToolCalls)
	fmt.Fprintf(&b, "- **Feedback rate:** %.1f%%\n", stats.FeedbackRate*100)
	if stats.FeedbackRate > 0 {
		fmt.Fprintf(&b, "- **Thumbs-up rate:** %.1f%%\n", stats.ThumbsUpRate*100)
	}
	b.WriteString("\n")

	// Outliers
	if len(outliers) > 0 {
		b.WriteString("## Duration Outliers\n\n")
		b.WriteString("| Trace ID | Value (s) | Threshold (s) | Direction |\n")
		b.WriteString("|----------|-----------|---------------|----------|\n")
		for _, o := range outliers {
			fmt.Fprintf(&b, "| %s | %.2f | %.2f | %s |\n", o.TraceID, o.Value, o.Threshold, o.Direction)
		}
		b.WriteString("\n")
	}

	// Metadata breakdown
	if len(stats.ByMetadata) > 0 {
		b.WriteString("## Metadata Breakdown\n\n")
		// Sort keys for deterministic output.
		metaKeys := make([]string, 0, len(stats.ByMetadata))
		for k := range stats.ByMetadata {
			metaKeys = append(metaKeys, k)
		}
		sort.Strings(metaKeys)

		for _, k := range metaKeys {
			vals := stats.ByMetadata[k]
			fmt.Fprintf(&b, "### %s\n\n", k)
			// Sort value keys.
			vKeys := make([]string, 0, len(vals))
			for vk := range vals {
				vKeys = append(vKeys, vk)
			}
			sort.Strings(vKeys)
			for _, vk := range vKeys {
				fmt.Fprintf(&b, "- %s: %d\n", vk, vals[vk])
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}
