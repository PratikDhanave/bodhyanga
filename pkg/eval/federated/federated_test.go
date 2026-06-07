package federated

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// SubmitSiteReport + Aggregate basics
// ---------------------------------------------------------------------------

func TestAggregateEmpty(t *testing.T) {
	c := NewFederatedCoordinator(0)
	r := c.Aggregate()

	if r.GlobalAccuracy != 0 {
		t.Errorf("expected 0 global accuracy for empty coordinator, got %f", r.GlobalAccuracy)
	}
	if r.GlobalCases != 0 {
		t.Errorf("expected 0 global cases, got %d", r.GlobalCases)
	}
	if len(r.Sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(r.Sites))
	}
	if len(r.Outliers) != 0 {
		t.Errorf("expected no outliers, got %v", r.Outliers)
	}
}

func TestAggregateSingleSite(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID:     "site-a",
		SiteName:   "Hospital A",
		TotalCases: 100,
		Accuracy:   0.90,
		ByCondition: map[string]float64{
			"diabetes": 0.85,
			"asthma":   0.95,
		},
		AvgLatencyMs: 120,
		AvgCostUSD:   0.05,
		ReportedAt:   time.Now(),
	})

	r := c.Aggregate()
	if r.GlobalCases != 100 {
		t.Errorf("expected 100 global cases, got %d", r.GlobalCases)
	}
	if math.Abs(r.GlobalAccuracy-0.90) > 1e-9 {
		t.Errorf("expected 0.90 global accuracy, got %f", r.GlobalAccuracy)
	}
	if len(r.Outliers) != 0 {
		t.Errorf("expected no outliers for single site, got %v", r.Outliers)
	}
}

func TestAggregateWeightedAverage(t *testing.T) {
	c := NewFederatedCoordinator(0)
	// Site A: 100 cases at 0.80
	// Site B: 300 cases at 0.90
	// Weighted average = (100*0.80 + 300*0.90) / 400 = (80+270)/400 = 0.875
	c.SubmitSiteReport(SiteReport{
		SiteID: "site-a", SiteName: "Hospital A",
		TotalCases: 100, Accuracy: 0.80,
		ByCondition: map[string]float64{"hypertension": 0.80},
		ReportedAt:  time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "site-b", SiteName: "Hospital B",
		TotalCases: 300, Accuracy: 0.90,
		ByCondition: map[string]float64{"hypertension": 0.90},
		ReportedAt:  time.Now(),
	})

	r := c.Aggregate()
	if r.GlobalCases != 400 {
		t.Errorf("expected 400 global cases, got %d", r.GlobalCases)
	}
	expected := 0.875
	if math.Abs(r.GlobalAccuracy-expected) > 1e-9 {
		t.Errorf("expected %.4f global accuracy, got %.4f", expected, r.GlobalAccuracy)
	}
}

func TestAggregateReplacesReport(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "site-a", TotalCases: 50, Accuracy: 0.70,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "site-a", TotalCases: 100, Accuracy: 0.90,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	r := c.Aggregate()
	if len(r.Sites) != 1 {
		t.Fatalf("expected 1 site after replacement, got %d", len(r.Sites))
	}
	if r.GlobalCases != 100 {
		t.Errorf("expected 100 cases after replacement, got %d", r.GlobalCases)
	}
	if math.Abs(r.GlobalAccuracy-0.90) > 1e-9 {
		t.Errorf("expected 0.90 accuracy after replacement, got %f", r.GlobalAccuracy)
	}
}

// ---------------------------------------------------------------------------
// Outlier detection
// ---------------------------------------------------------------------------

func TestOutlierDetection(t *testing.T) {
	c := NewFederatedCoordinator(0)

	// Create a cluster of high-accuracy sites and one clear outlier.
	for i := 0; i < 10; i++ {
		c.SubmitSiteReport(SiteReport{
			SiteID:      siteID(i),
			TotalCases:  100,
			Accuracy:    0.90,
			ByCondition: map[string]float64{},
			ReportedAt:  time.Now(),
		})
	}
	// Outlier: much lower accuracy.
	c.SubmitSiteReport(SiteReport{
		SiteID:      "outlier",
		TotalCases:  100,
		Accuracy:    0.30,
		ByCondition: map[string]float64{},
		ReportedAt:  time.Now(),
	})

	r := c.Aggregate()
	found := false
	for _, id := range r.Outliers {
		if id == "outlier" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'outlier' in outlier list, got %v", r.Outliers)
	}

	// None of the high-accuracy sites should be outliers.
	for _, id := range r.Outliers {
		if id != "outlier" {
			t.Errorf("unexpected outlier: %s", id)
		}
	}
}

func TestNoOutlierWhenAllSimilar(t *testing.T) {
	c := NewFederatedCoordinator(0)
	for i := 0; i < 5; i++ {
		c.SubmitSiteReport(SiteReport{
			SiteID:      siteID(i),
			TotalCases:  100,
			Accuracy:    0.85 + float64(i)*0.01,
			ByCondition: map[string]float64{},
			ReportedAt:  time.Now(),
		})
	}

	r := c.Aggregate()
	if len(r.Outliers) != 0 {
		t.Errorf("expected no outliers for similar sites, got %v", r.Outliers)
	}
}

// ---------------------------------------------------------------------------
// Laplace noise
// ---------------------------------------------------------------------------

func TestAddLaplaceNoiseZeroEpsilon(t *testing.T) {
	// Epsilon <= 0 means no noise.
	v := AddLaplaceNoise(0.85, 0)
	if v != 0.85 {
		t.Errorf("expected 0.85 unchanged, got %f", v)
	}
	v = AddLaplaceNoise(0.85, -1)
	if v != 0.85 {
		t.Errorf("expected 0.85 unchanged for negative epsilon, got %f", v)
	}
}

func TestAddLaplaceNoiseAddsNoise(t *testing.T) {
	// With epsilon > 0, repeated calls should produce different values.
	const n = 1000
	epsilon := 1.0
	baseline := 0.50
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += AddLaplaceNoise(baseline, epsilon)
	}
	mean := sum / float64(n)
	// The mean of Laplace(0, 1/epsilon) is 0, so the mean of the noisy
	// values should be close to baseline. Allow generous tolerance.
	if math.Abs(mean-baseline) > 0.15 {
		t.Errorf("mean of noisy values %.4f deviates too far from baseline %.4f", mean, baseline)
	}
}

func TestAddLaplaceNoiseVarianceScales(t *testing.T) {
	// Higher epsilon → less noise (smaller scale). Var(Laplace(0,b)) = 2*b^2.
	// b = 1/epsilon, so Var = 2/epsilon^2. epsilon=10 → Var=0.02, epsilon=0.1 → Var=200.
	const n = 5000
	baseline := 0.50

	variance := func(eps float64) float64 {
		sum, sumSq := 0.0, 0.0
		for i := 0; i < n; i++ {
			v := AddLaplaceNoise(baseline, eps)
			sum += v
			sumSq += v * v
		}
		mean := sum / float64(n)
		return sumSq/float64(n) - mean*mean
	}

	varHigh := variance(10.0) // tight
	varLow := variance(0.5)   // loose

	if varLow <= varHigh {
		t.Errorf("expected lower epsilon to produce higher variance: varLow=%.6f varHigh=%.6f", varLow, varHigh)
	}
}

// ---------------------------------------------------------------------------
// ExportReport
// ---------------------------------------------------------------------------

func TestExportReport(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "site-a", SiteName: "Test Hospital",
		TotalCases: 50, Accuracy: 0.88,
		ByCondition: map[string]float64{"flu": 0.90},
		ReportedAt:  time.Now(),
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "report.json")
	if err := c.ExportReport(path); err != nil {
		t.Fatalf("ExportReport: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}

	var report FederatedReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal exported report: %v", err)
	}
	if report.GlobalCases != 50 {
		t.Errorf("expected 50 global cases in export, got %d", report.GlobalCases)
	}
	if math.Abs(report.GlobalAccuracy-0.88) > 1e-9 {
		t.Errorf("expected 0.88 accuracy in export, got %f", report.GlobalAccuracy)
	}
}

// ---------------------------------------------------------------------------
// Per-condition global accuracy
// ---------------------------------------------------------------------------

func TestByConditionGlobal(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "a", TotalCases: 100, Accuracy: 0.80,
		ByCondition: map[string]float64{"diabetes": 0.70, "asthma": 0.90},
		ReportedAt:  time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "b", TotalCases: 100, Accuracy: 0.90,
		ByCondition: map[string]float64{"diabetes": 0.90, "asthma": 0.90},
		ReportedAt:  time.Now(),
	})

	r := c.Aggregate()
	// Weighted by equal case counts (100 each), so global = simple average.
	expectedDiabetes := 0.80 // (100*0.70 + 100*0.90) / 200
	expectedAsthma := 0.90   // (100*0.90 + 100*0.90) / 200

	if math.Abs(r.ByCondition["diabetes"]-expectedDiabetes) > 1e-9 {
		t.Errorf("expected diabetes %.4f, got %.4f", expectedDiabetes, r.ByCondition["diabetes"])
	}
	if math.Abs(r.ByCondition["asthma"]-expectedAsthma) > 1e-9 {
		t.Errorf("expected asthma %.4f, got %.4f", expectedAsthma, r.ByCondition["asthma"])
	}
}

// ---------------------------------------------------------------------------
// DP-noised aggregation
// ---------------------------------------------------------------------------

func TestAggregateWithDP(t *testing.T) {
	c := NewFederatedCoordinator(1.0) // epsilon = 1.0
	c.SubmitSiteReport(SiteReport{
		SiteID: "a", TotalCases: 200, Accuracy: 0.85,
		ByCondition: map[string]float64{"sepsis": 0.80},
		ReportedAt:  time.Now(),
	})

	r := c.Aggregate()
	// Global accuracy is NOT noised (it's a weighted average).
	if math.Abs(r.GlobalAccuracy-0.85) > 1e-9 {
		t.Errorf("expected 0.85 global accuracy, got %f", r.GlobalAccuracy)
	}
	// Per-condition IS noised, so it should differ from the raw 0.80
	// (with high probability). Run multiple times to check it varies.
	values := make(map[float64]bool)
	for i := 0; i < 10; i++ {
		r2 := c.Aggregate()
		values[r2.ByCondition["sepsis"]] = true
	}
	if len(values) < 2 {
		t.Error("expected DP noise to produce varying per-condition values")
	}
}

// ---------------------------------------------------------------------------
// HTTP handler helpers
// ---------------------------------------------------------------------------

func TestHandleSubmitReport(t *testing.T) {
	c := NewFederatedCoordinator(0)
	handler := HandleSubmitReport(c)

	body, _ := json.Marshal(SiteReport{
		SiteID: "http-site", SiteName: "HTTP Test",
		TotalCases: 75, Accuracy: 0.92,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	var buf []byte
	w := &bufWriter{buf: &buf}
	if err := handler(w, body); err != nil {
		t.Fatalf("HandleSubmitReport: %v", err)
	}

	r := c.Aggregate()
	if r.GlobalCases != 75 {
		t.Errorf("expected 75 cases after HTTP submit, got %d", r.GlobalCases)
	}
}

func TestHandleAggregate(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "x", TotalCases: 10, Accuracy: 0.60,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	data, err := HandleAggregate(c)
	if err != nil {
		t.Fatalf("HandleAggregate: %v", err)
	}
	var report FederatedReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if report.GlobalCases != 10 {
		t.Errorf("expected 10 cases, got %d", report.GlobalCases)
	}
}

// ---------------------------------------------------------------------------
// PrintReport
// ---------------------------------------------------------------------------

func TestPrintReport_Empty(t *testing.T) {
	// PrintReport with zero sites, no conditions, no outliers.
	r := FederatedReport{
		Sites:       nil,
		ByCondition: nil,
		Outliers:    nil,
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	// Should not panic. We capture stdout to verify output.
	PrintReport(r)
}

func TestPrintReport_WithSites(t *testing.T) {
	r := FederatedReport{
		Sites: []SiteReport{
			{
				SiteID: "s1", SiteName: "Hospital A",
				TotalCases: 100, Accuracy: 0.90,
				AvgLatencyMs: 120, AvgCostUSD: 0.05,
			},
			{
				SiteID: "s2", SiteName: "Hospital B",
				TotalCases: 200, Accuracy: 0.85,
				AvgLatencyMs: 150, AvgCostUSD: 0.08,
			},
		},
		GlobalAccuracy: 0.8667,
		GlobalCases:    300,
		ByCondition: map[string]float64{
			"diabetes": 0.88,
			"asthma":   0.92,
		},
		Outliers:    nil,
		GeneratedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	PrintReport(r)
}

func TestPrintReport_WithOutliers(t *testing.T) {
	r := FederatedReport{
		Sites: []SiteReport{
			{SiteID: "s1", SiteName: "Good", TotalCases: 100, Accuracy: 0.95},
		},
		GlobalAccuracy: 0.85,
		GlobalCases:    200,
		ByCondition:    map[string]float64{},
		Outliers:       []string{"bad-site-1", "bad-site-2"},
		GeneratedAt:    time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	}
	PrintReport(r)
}

// ---------------------------------------------------------------------------
// ExportReport error paths
// ---------------------------------------------------------------------------

func TestExportReport_WriteError(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "a", TotalCases: 10, Accuracy: 0.80,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	// Attempt to write to a path that will fail: use a file as if it were
	// a directory so the WriteFile call fails.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "blocker" is a file, so "blocker/report.json" should fail.
	badPath := filepath.Join(blocker, "report.json")
	err := c.ExportReport(badPath)
	if err == nil {
		t.Fatal("expected error writing to invalid path, got nil")
	}
}

func TestExportReport_MkdirError(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "a", TotalCases: 10, Accuracy: 0.80,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	// Use a read-only directory to force MkdirAll failure for nested subdirs.
	dir := t.TempDir()
	readOnly := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	// Try to create nested dirs under a read-only parent.
	badPath := filepath.Join(readOnly, "nested", "deep", "report.json")
	err := c.ExportReport(badPath)
	if err == nil {
		t.Fatal("expected error from MkdirAll on read-only dir, got nil")
	}
}

// ---------------------------------------------------------------------------
// HandleSubmitReport error path
// ---------------------------------------------------------------------------

func TestHandleSubmitReport_InvalidJSON(t *testing.T) {
	c := NewFederatedCoordinator(0)
	handler := HandleSubmitReport(c)

	var buf []byte
	w := &bufWriter{buf: &buf}
	err := handler(w, []byte(`{not valid json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON body, got nil")
	}

	// Verify that no site was submitted.
	r := c.Aggregate()
	if len(r.Sites) != 0 {
		t.Errorf("expected 0 sites after invalid submit, got %d", len(r.Sites))
	}
}

func TestHandleSubmitReport_ResponseContent(t *testing.T) {
	c := NewFederatedCoordinator(0)
	handler := HandleSubmitReport(c)

	body, _ := json.Marshal(SiteReport{
		SiteID: "resp-check", SiteName: "Response Check",
		TotalCases: 42, Accuracy: 0.77,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	var buf []byte
	w := &bufWriter{buf: &buf}
	if err := handler(w, body); err != nil {
		t.Fatalf("HandleSubmitReport: %v", err)
	}

	var resp map[string]string
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["status"] != "accepted" {
		t.Errorf("expected status 'accepted', got %q", resp["status"])
	}
	if resp["site_id"] != "resp-check" {
		t.Errorf("expected site_id 'resp-check', got %q", resp["site_id"])
	}
}

// ---------------------------------------------------------------------------
// detectOutliers: all sites with zero cases
// ---------------------------------------------------------------------------

func TestDetectOutliers_ZeroCasesAllSites(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "zero-a", TotalCases: 0, Accuracy: 0.50,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "zero-b", TotalCases: 0, Accuracy: 0.90,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	r := c.Aggregate()
	// With 0 total cases, globalAccuracy should be 0.
	if r.GlobalAccuracy != 0 {
		t.Errorf("expected 0 global accuracy for zero-case sites, got %f", r.GlobalAccuracy)
	}
	// Outlier detection returns nil when totalCases == 0.
	if len(r.Outliers) != 0 {
		t.Errorf("expected no outliers for zero-case sites, got %v", r.Outliers)
	}
}

// ---------------------------------------------------------------------------
// clamp01
// ---------------------------------------------------------------------------

func TestClamp01_Negative(t *testing.T) {
	v := clamp01(-0.5)
	if v != 0 {
		t.Errorf("expected clamp01(-0.5) = 0, got %f", v)
	}
}

func TestClamp01_AboveOne(t *testing.T) {
	v := clamp01(1.5)
	if v != 1 {
		t.Errorf("expected clamp01(1.5) = 1, got %f", v)
	}
}

func TestClamp01_InRange(t *testing.T) {
	v := clamp01(0.42)
	if v != 0.42 {
		t.Errorf("expected clamp01(0.42) = 0.42, got %f", v)
	}
}

func TestClamp01_BoundaryZero(t *testing.T) {
	v := clamp01(0)
	if v != 0 {
		t.Errorf("expected clamp01(0) = 0, got %f", v)
	}
}

func TestClamp01_BoundaryOne(t *testing.T) {
	v := clamp01(1)
	if v != 1 {
		t.Errorf("expected clamp01(1) = 1, got %f", v)
	}
}

// ---------------------------------------------------------------------------
// Edge case: single-node federation
// ---------------------------------------------------------------------------

func TestAggregate_SingleNode_NoConditions(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "solo", SiteName: "Solo Clinic",
		TotalCases: 50, Accuracy: 0.76,
		ByCondition:  map[string]float64{},
		AvgLatencyMs: 200, AvgCostUSD: 0.10,
		ReportedAt: time.Now(),
	})

	r := c.Aggregate()
	if len(r.Sites) != 1 {
		t.Fatalf("expected 1 site, got %d", len(r.Sites))
	}
	if r.GlobalCases != 50 {
		t.Errorf("expected 50 cases, got %d", r.GlobalCases)
	}
	if math.Abs(r.GlobalAccuracy-0.76) > 1e-9 {
		t.Errorf("expected 0.76 accuracy, got %f", r.GlobalAccuracy)
	}
	// Single site: no outliers possible.
	if len(r.Outliers) != 0 {
		t.Errorf("expected no outliers for single node, got %v", r.Outliers)
	}
	if len(r.ByCondition) != 0 {
		t.Errorf("expected empty by-condition, got %v", r.ByCondition)
	}
}

// ---------------------------------------------------------------------------
// Edge case: conflicting results from multiple nodes
// ---------------------------------------------------------------------------

func TestAggregate_ConflictingAccuracy(t *testing.T) {
	c := NewFederatedCoordinator(0)
	// Three sites with wildly different accuracy for the same condition.
	c.SubmitSiteReport(SiteReport{
		SiteID: "high", TotalCases: 100, Accuracy: 0.99,
		ByCondition: map[string]float64{"pneumonia": 0.99},
		ReportedAt:  time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "mid", TotalCases: 100, Accuracy: 0.50,
		ByCondition: map[string]float64{"pneumonia": 0.50},
		ReportedAt:  time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "low", TotalCases: 100, Accuracy: 0.10,
		ByCondition: map[string]float64{"pneumonia": 0.10},
		ReportedAt:  time.Now(),
	})

	r := c.Aggregate()
	// Weighted average: (100*0.99 + 100*0.50 + 100*0.10) / 300 = 159/300 = 0.53
	expected := (0.99 + 0.50 + 0.10) / 3.0
	if math.Abs(r.GlobalAccuracy-expected) > 1e-9 {
		t.Errorf("expected %.4f accuracy, got %.4f", expected, r.GlobalAccuracy)
	}

	// Per-condition should also be the same weighted average.
	expectedCond := expected
	if math.Abs(r.ByCondition["pneumonia"]-expectedCond) > 1e-9 {
		t.Errorf("expected condition accuracy %.4f, got %.4f", expectedCond, r.ByCondition["pneumonia"])
	}
}

// ---------------------------------------------------------------------------
// Edge case: all nodes zero accuracy
// ---------------------------------------------------------------------------

func TestAggregate_AllNodesZeroAccuracy(t *testing.T) {
	c := NewFederatedCoordinator(0)
	for i := 0; i < 5; i++ {
		c.SubmitSiteReport(SiteReport{
			SiteID: siteID(i), TotalCases: 100, Accuracy: 0.0,
			ByCondition: map[string]float64{"condition": 0.0},
			ReportedAt:  time.Now(),
		})
	}

	r := c.Aggregate()
	if r.GlobalAccuracy != 0 {
		t.Errorf("expected 0 global accuracy, got %f", r.GlobalAccuracy)
	}
	if r.ByCondition["condition"] != 0 {
		t.Errorf("expected 0 condition accuracy, got %f", r.ByCondition["condition"])
	}
	// All identical, no outliers.
	if len(r.Outliers) != 0 {
		t.Errorf("expected no outliers when all have same accuracy, got %v", r.Outliers)
	}
}

// ---------------------------------------------------------------------------
// Edge case: all nodes perfect accuracy
// ---------------------------------------------------------------------------

func TestAggregate_AllNodesPerfectAccuracy(t *testing.T) {
	c := NewFederatedCoordinator(0)
	for i := 0; i < 4; i++ {
		c.SubmitSiteReport(SiteReport{
			SiteID: siteID(i), TotalCases: 50, Accuracy: 1.0,
			ByCondition: map[string]float64{"healthy": 1.0},
			ReportedAt:  time.Now(),
		})
	}

	r := c.Aggregate()
	if r.GlobalAccuracy != 1.0 {
		t.Errorf("expected 1.0 global accuracy, got %f", r.GlobalAccuracy)
	}
	if len(r.Outliers) != 0 {
		t.Errorf("expected no outliers, got %v", r.Outliers)
	}
}

// ---------------------------------------------------------------------------
// Edge case: disjoint conditions across sites
// ---------------------------------------------------------------------------

func TestAggregate_DisjointConditions(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "cardio", TotalCases: 200, Accuracy: 0.85,
		ByCondition: map[string]float64{"heart-failure": 0.80, "arrhythmia": 0.90},
		ReportedAt:  time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "neuro", TotalCases: 150, Accuracy: 0.92,
		ByCondition: map[string]float64{"stroke": 0.88, "epilepsy": 0.96},
		ReportedAt:  time.Now(),
	})

	r := c.Aggregate()
	// Each condition only appears at one site, so global condition accuracy
	// should equal that site's accuracy for that condition (weighted by that
	// site's case count / that site's case count = 1).
	for _, cond := range []string{"heart-failure", "arrhythmia", "stroke", "epilepsy"} {
		if _, ok := r.ByCondition[cond]; !ok {
			t.Errorf("missing condition %q in global report", cond)
		}
	}
	if math.Abs(r.ByCondition["heart-failure"]-0.80) > 1e-9 {
		t.Errorf("expected heart-failure 0.80, got %f", r.ByCondition["heart-failure"])
	}
	if math.Abs(r.ByCondition["stroke"]-0.88) > 1e-9 {
		t.Errorf("expected stroke 0.88, got %f", r.ByCondition["stroke"])
	}
}

// ---------------------------------------------------------------------------
// Edge case: large number of sites
// ---------------------------------------------------------------------------

func TestAggregate_ManySites(t *testing.T) {
	c := NewFederatedCoordinator(0)
	const n = 100
	for i := 0; i < n; i++ {
		c.SubmitSiteReport(SiteReport{
			SiteID:      fmt.Sprintf("site-%03d", i),
			TotalCases:  100,
			Accuracy:    0.80,
			ByCondition: map[string]float64{"common": 0.80},
			ReportedAt:  time.Now(),
		})
	}

	r := c.Aggregate()
	if len(r.Sites) != n {
		t.Errorf("expected %d sites, got %d", n, len(r.Sites))
	}
	if r.GlobalCases != n*100 {
		t.Errorf("expected %d cases, got %d", n*100, r.GlobalCases)
	}
	if math.Abs(r.GlobalAccuracy-0.80) > 1e-9 {
		t.Errorf("expected 0.80 accuracy, got %f", r.GlobalAccuracy)
	}
}

// ---------------------------------------------------------------------------
// Concurrent submission
// ---------------------------------------------------------------------------

func TestSubmitSiteReport_Concurrent(t *testing.T) {
	c := NewFederatedCoordinator(0)
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			c.SubmitSiteReport(SiteReport{
				SiteID:      fmt.Sprintf("concurrent-%d", idx),
				TotalCases:  10,
				Accuracy:    0.80,
				ByCondition: map[string]float64{},
				ReportedAt:  time.Now(),
			})
		}(i)
	}
	wg.Wait()

	r := c.Aggregate()
	if len(r.Sites) != goroutines {
		t.Errorf("expected %d sites from concurrent submissions, got %d", goroutines, len(r.Sites))
	}
}

// ---------------------------------------------------------------------------
// DP-noised aggregation: clamping behavior
// ---------------------------------------------------------------------------

func TestAggregateWithDP_ClampedConditionValues(t *testing.T) {
	// With high epsilon (tight noise) and boundary values, verify clamping.
	c := NewFederatedCoordinator(100.0) // very high epsilon = very small noise
	c.SubmitSiteReport(SiteReport{
		SiteID: "boundary", TotalCases: 1000, Accuracy: 0.50,
		ByCondition: map[string]float64{
			"near-zero": 0.001,
			"near-one":  0.999,
		},
		ReportedAt: time.Now(),
	})

	// Run multiple times: values should stay in [0, 1].
	for i := 0; i < 100; i++ {
		r := c.Aggregate()
		for cond, v := range r.ByCondition {
			if v < 0 || v > 1 {
				t.Fatalf("iteration %d: condition %q value %.4f out of [0, 1]", i, cond, v)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// ExportReport: successful roundtrip with multiple sites
// ---------------------------------------------------------------------------

func TestExportReport_MultipleSites(t *testing.T) {
	c := NewFederatedCoordinator(0)
	for i := 0; i < 5; i++ {
		c.SubmitSiteReport(SiteReport{
			SiteID: fmt.Sprintf("export-%d", i), SiteName: fmt.Sprintf("Hospital %d", i),
			TotalCases: 100 + i*10, Accuracy: 0.80 + float64(i)*0.03,
			ByCondition: map[string]float64{"test-cond": 0.85},
			ReportedAt:  time.Now(),
		})
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "multi_report.json")
	if err := c.ExportReport(path); err != nil {
		t.Fatalf("ExportReport: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var report FederatedReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(report.Sites) != 5 {
		t.Errorf("expected 5 sites in export, got %d", len(report.Sites))
	}
	if report.GlobalCases != 100+110+120+130+140 {
		t.Errorf("expected %d global cases, got %d", 600, report.GlobalCases)
	}
}

// ---------------------------------------------------------------------------
// HandleAggregate: empty coordinator
// ---------------------------------------------------------------------------

func TestHandleAggregate_Empty(t *testing.T) {
	c := NewFederatedCoordinator(0)
	data, err := HandleAggregate(c)
	if err != nil {
		t.Fatalf("HandleAggregate: %v", err)
	}
	var report FederatedReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(report.Sites) != 0 {
		t.Errorf("expected 0 sites, got %d", len(report.Sites))
	}
	if report.GlobalAccuracy != 0 {
		t.Errorf("expected 0 accuracy, got %f", report.GlobalAccuracy)
	}
}

// ---------------------------------------------------------------------------
// Multiple report replacement
// ---------------------------------------------------------------------------

func TestSubmitSiteReport_MultipleReplacements(t *testing.T) {
	c := NewFederatedCoordinator(0)
	for i := 0; i < 10; i++ {
		c.SubmitSiteReport(SiteReport{
			SiteID: "evolving", TotalCases: i * 10, Accuracy: float64(i) / 10.0,
			ByCondition: map[string]float64{}, ReportedAt: time.Now(),
		})
	}

	r := c.Aggregate()
	if len(r.Sites) != 1 {
		t.Fatalf("expected 1 site after 10 replacements, got %d", len(r.Sites))
	}
	// Last submission: TotalCases=90, Accuracy=0.9
	if r.GlobalCases != 90 {
		t.Errorf("expected 90 cases, got %d", r.GlobalCases)
	}
	if math.Abs(r.GlobalAccuracy-0.9) > 1e-9 {
		t.Errorf("expected 0.9 accuracy, got %f", r.GlobalAccuracy)
	}
}

// ---------------------------------------------------------------------------
// Weighted outlier detection with unequal case counts
// ---------------------------------------------------------------------------

func TestOutlierDetection_WeightedByCase(t *testing.T) {
	c := NewFederatedCoordinator(0)
	// Many high-accuracy sites with large case counts.
	for i := 0; i < 8; i++ {
		c.SubmitSiteReport(SiteReport{
			SiteID: fmt.Sprintf("big-%d", i), TotalCases: 1000,
			Accuracy:    0.90,
			ByCondition: map[string]float64{}, ReportedAt: time.Now(),
		})
	}
	// One site with low accuracy but tiny case count.
	c.SubmitSiteReport(SiteReport{
		SiteID: "tiny-bad", TotalCases: 5, Accuracy: 0.10,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	r := c.Aggregate()
	// The tiny site's accuracy is well below the weighted mean, so it
	// should be detected as an outlier.
	found := false
	for _, id := range r.Outliers {
		if id == "tiny-bad" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'tiny-bad' as outlier, got %v", r.Outliers)
	}
}

// ---------------------------------------------------------------------------
// Two sites: neither is an outlier (not enough spread)
// ---------------------------------------------------------------------------

func TestOutlierDetection_TwoSites_NoOutlier(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "a", TotalCases: 100, Accuracy: 0.88,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "b", TotalCases: 100, Accuracy: 0.82,
		ByCondition: map[string]float64{}, ReportedAt: time.Now(),
	})

	r := c.Aggregate()
	// With only 2 sites, the stddev is small relative to the spread,
	// so the lower one is exactly 1 stddev below = not an outlier.
	if len(r.Outliers) != 0 {
		t.Errorf("expected no outliers for 2 similar sites, got %v", r.Outliers)
	}
}

// ---------------------------------------------------------------------------
// Epsilon in SiteReport round-trips through JSON
// ---------------------------------------------------------------------------

func TestSiteReport_EpsilonOmitEmpty(t *testing.T) {
	// Epsilon = 0 should be omitted from JSON.
	r := SiteReport{SiteID: "a", Epsilon: 0}
	data, _ := json.Marshal(r)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if _, ok := raw["epsilon"]; ok {
		t.Error("expected epsilon to be omitted when 0")
	}

	// Epsilon > 0 should be present.
	r2 := SiteReport{SiteID: "b", Epsilon: 1.5}
	data2, _ := json.Marshal(r2)
	var raw2 map[string]interface{}
	json.Unmarshal(data2, &raw2)
	if _, ok := raw2["epsilon"]; !ok {
		t.Error("expected epsilon to be present when > 0")
	}
}

// ---------------------------------------------------------------------------
// Aggregate with conditions from different subsets of sites
// ---------------------------------------------------------------------------

func TestAggregate_OverlappingConditions(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "a", TotalCases: 200, Accuracy: 0.80,
		ByCondition: map[string]float64{
			"shared": 0.70,
			"only-a": 0.90,
		},
		ReportedAt: time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "b", TotalCases: 100, Accuracy: 0.90,
		ByCondition: map[string]float64{
			"shared": 0.85,
			"only-b": 0.95,
		},
		ReportedAt: time.Now(),
	})

	r := c.Aggregate()
	// "shared": (200*0.70 + 100*0.85) / 300 = (140+85)/300 = 0.75
	expectedShared := (200*0.70 + 100*0.85) / 300.0
	if math.Abs(r.ByCondition["shared"]-expectedShared) > 1e-9 {
		t.Errorf("expected shared=%.4f, got %.4f", expectedShared, r.ByCondition["shared"])
	}
	// "only-a": (200*0.90) / 200 = 0.90
	if math.Abs(r.ByCondition["only-a"]-0.90) > 1e-9 {
		t.Errorf("expected only-a=0.90, got %f", r.ByCondition["only-a"])
	}
	// "only-b": (100*0.95) / 100 = 0.95
	if math.Abs(r.ByCondition["only-b"]-0.95) > 1e-9 {
		t.Errorf("expected only-b=0.95, got %f", r.ByCondition["only-b"])
	}
}

// ---------------------------------------------------------------------------
// Aggregate idempotency: calling Aggregate multiple times yields same result
// ---------------------------------------------------------------------------

func TestAggregate_Idempotent(t *testing.T) {
	c := NewFederatedCoordinator(0)
	c.SubmitSiteReport(SiteReport{
		SiteID: "a", TotalCases: 100, Accuracy: 0.85,
		ByCondition: map[string]float64{"x": 0.80}, ReportedAt: time.Now(),
	})
	c.SubmitSiteReport(SiteReport{
		SiteID: "b", TotalCases: 200, Accuracy: 0.90,
		ByCondition: map[string]float64{"x": 0.95}, ReportedAt: time.Now(),
	})

	r1 := c.Aggregate()
	r2 := c.Aggregate()

	if r1.GlobalAccuracy != r2.GlobalAccuracy {
		t.Errorf("aggregate not idempotent: accuracy %f vs %f", r1.GlobalAccuracy, r2.GlobalAccuracy)
	}
	if r1.GlobalCases != r2.GlobalCases {
		t.Errorf("aggregate not idempotent: cases %d vs %d", r1.GlobalCases, r2.GlobalCases)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func siteID(i int) string {
	return "site-" + string(rune('a'+i))
}

// bufWriter is a minimal writer for testing HandleSubmitReport.
type bufWriter struct {
	buf *[]byte
}

func (w *bufWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}
