package federated

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
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
