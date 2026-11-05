// Package federated implements federated evaluation for Bodhyanga. It runs
// accuracy benchmarks across deployment sites without sharing patient data —
// only aggregated metrics are exchanged between participants and the
// coordinating server.
//
// The coordinator collects per-site accuracy reports, computes a
// weighted-average global accuracy, detects outlier sites (accuracy more than
// 2 standard deviations below the mean), and optionally applies Laplace-noise
// differential privacy so that case-mix details cannot be reverse-engineered
// from the aggregated numbers.
//
// Typical usage:
//
//	coord := federated.NewFederatedCoordinator(0) // no DP noise
//	coord.SubmitSiteReport(federated.SiteReport{...})
//	report := coord.Aggregate()
//	coord.ExportReport("federated_report.json")
package federated

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// SiteReport is the per-site payload submitted by each deployment site.
// It contains only aggregated metrics — no patient-level data.
type SiteReport struct {
	SiteID      string             `json:"site_id"`
	SiteName    string             `json:"site_name"`
	TotalCases  int                `json:"total_cases"`
	Accuracy    float64            `json:"accuracy"`
	ByCondition map[string]float64 `json:"by_condition"`
	AvgLatencyMs float64           `json:"avg_latency_ms"`
	AvgCostUSD  float64            `json:"avg_cost_usd"`
	ReportedAt  time.Time          `json:"reported_at"`
	// Epsilon is the differential-privacy noise parameter applied by the site
	// before submission (0 means no noise was added).
	Epsilon float64 `json:"epsilon,omitempty"`
}

// FederatedReport is the coordinator's aggregated output across all sites.
type FederatedReport struct {
	Sites          []SiteReport       `json:"sites"`
	GlobalAccuracy float64            `json:"global_accuracy"`
	GlobalCases    int                `json:"global_cases"`
	ByCondition    map[string]float64 `json:"by_condition_global"`
	Outliers       []string           `json:"outlier_sites"`
	GeneratedAt    time.Time          `json:"generated_at"`
}

// FederatedCoordinator collects site reports and produces aggregated
// federated evaluation results. It is safe for concurrent use.
type FederatedCoordinator struct {
	reports map[string]SiteReport
	mu      sync.RWMutex
	epsilon float64 // differential privacy parameter (0 = disabled)
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewFederatedCoordinator creates a coordinator. When epsilon > 0, Laplace
// noise is applied to the per-condition accuracy values in the aggregated
// report to provide differential-privacy guarantees.
func NewFederatedCoordinator(epsilon float64) *FederatedCoordinator {
	return &FederatedCoordinator{
		reports: make(map[string]SiteReport),
		epsilon: epsilon,
	}
}

// ---------------------------------------------------------------------------
// Submit / Aggregate
// ---------------------------------------------------------------------------

// SubmitSiteReport records (or replaces) a site's aggregated metrics.
// Concurrent calls from different goroutines are safe.
func (c *FederatedCoordinator) SubmitSiteReport(r SiteReport) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reports[r.SiteID] = r
}

// Aggregate computes the global federated evaluation report from all
// submitted site reports. The global accuracy is a case-count-weighted
// average. Outlier sites are those whose accuracy lies more than 2 standard
// deviations below the (weighted) mean.
func (c *FederatedCoordinator) Aggregate() FederatedReport {
	c.mu.RLock()
	defer c.mu.RUnlock()

	sites := make([]SiteReport, 0, len(c.reports))
	for _, r := range c.reports {
		sites = append(sites, r)
	}

	if len(sites) == 0 {
		return FederatedReport{
			Sites:       sites,
			ByCondition: make(map[string]float64),
			GeneratedAt: time.Now().UTC(),
		}
	}

	// ── Weighted global accuracy ──────────────────────────────────────────
	var totalCases int
	var weightedSum float64
	for _, s := range sites {
		totalCases += s.TotalCases
		weightedSum += float64(s.TotalCases) * s.Accuracy
	}
	globalAccuracy := 0.0
	if totalCases > 0 {
		globalAccuracy = weightedSum / float64(totalCases)
	}

	// ── Per-condition weighted accuracy ───────────────────────────────────
	condWeighted := make(map[string]float64) // condition → sum(cases * acc)
	condCases := make(map[string]int)        // condition → sum(cases)
	for _, s := range sites {
		for cond, acc := range s.ByCondition {
			// Use the site-level case count as the weight. In a more
			// refined model each condition would carry its own N; for the
			// federated prototype the site total is the best proxy.
			condWeighted[cond] += float64(s.TotalCases) * acc
			condCases[cond] += s.TotalCases
		}
	}
	byCondition := make(map[string]float64, len(condWeighted))
	for cond, w := range condWeighted {
		if condCases[cond] > 0 {
			v := w / float64(condCases[cond])
			if c.epsilon > 0 {
				v = AddLaplaceNoise(v, c.epsilon)
				v = clamp01(v)
			}
			byCondition[cond] = v
		}
	}

	// ── Outlier detection ─────────────────────────────────────────────────
	outliers := detectOutliers(sites, globalAccuracy)

	return FederatedReport{
		Sites:          sites,
		GlobalAccuracy: globalAccuracy,
		GlobalCases:    totalCases,
		ByCondition:    byCondition,
		Outliers:       outliers,
		GeneratedAt:    time.Now().UTC(),
	}
}

// ---------------------------------------------------------------------------
// Differential privacy
// ---------------------------------------------------------------------------

// AddLaplaceNoise adds Laplace(0, 1/epsilon) noise to value. This is the
// standard Laplace mechanism for epsilon-differential privacy. When epsilon
// is <= 0 the original value is returned unchanged.
func AddLaplaceNoise(value float64, epsilon float64) float64 {
	if epsilon <= 0 {
		return value
	}
	scale := 1.0 / epsilon
	return value + laplaceSample(scale)
}

// laplaceSample draws from Laplace(0, b) using inverse-CDF:
//
//	X = -b * sign(U) * ln(1 - 2|U|) where U ~ Uniform(-0.5, 0.5)
func laplaceSample(b float64) float64 {
	u := rand.Float64() - 0.5 // U in (-0.5, 0.5)
	sign := 1.0
	if u < 0 {
		sign = -1.0
		u = -u
	}
	return -b * sign * math.Log(1-2*u)
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

// ExportReport marshals the aggregated report to a JSON file at path.
// Parent directories are created if they do not exist.
func (c *FederatedCoordinator) ExportReport(path string) error {
	report := c.Aggregate()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("federated: mkdir %s: %w", filepath.Dir(path), err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("federated: marshal report: %w", err)
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("federated: write report %s: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTTP handler helpers
// ---------------------------------------------------------------------------

// HandleSubmitReport returns an http.HandlerFunc that accepts a JSON
// SiteReport via POST and submits it to the coordinator.
//
//	POST /api/federated/report
func HandleSubmitReport(c *FederatedCoordinator) func(w interface{ Write([]byte) (int, error) }, body []byte) error {
	// This is a pure function for easy testing; the cmd/ binary wires it
	// into net/http. See cmd/federated-eval for the full HTTP wiring.
	return func(w interface{ Write([]byte) (int, error) }, body []byte) error {
		var r SiteReport
		if err := json.Unmarshal(body, &r); err != nil {
			return fmt.Errorf("decode site report: %w", err)
		}
		c.SubmitSiteReport(r)
		resp, _ := json.Marshal(map[string]string{"status": "accepted", "site_id": r.SiteID})
		_, err := w.Write(resp)
		return err
	}
}

// HandleAggregate returns the aggregated FederatedReport as JSON.
//
//	GET /api/federated/aggregate
func HandleAggregate(c *FederatedCoordinator) ([]byte, error) {
	report := c.Aggregate()
	return json.Marshal(report)
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// detectOutliers returns the SiteIDs whose accuracy is more than 2 standard
// deviations below the weighted mean.
func detectOutliers(sites []SiteReport, mean float64) []string {
	if len(sites) < 2 {
		return nil
	}

	// Compute weighted standard deviation.
	var totalCases int
	for _, s := range sites {
		totalCases += s.TotalCases
	}
	if totalCases == 0 {
		return nil
	}

	var varianceSum float64
	for _, s := range sites {
		diff := s.Accuracy - mean
		varianceSum += float64(s.TotalCases) * diff * diff
	}
	stddev := math.Sqrt(varianceSum / float64(totalCases))

	threshold := mean - 2*stddev
	var outliers []string
	for _, s := range sites {
		if s.Accuracy < threshold {
			outliers = append(outliers, s.SiteID)
		}
	}
	return outliers
}

// clamp01 clamps a value to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// PrintReport writes a human-readable federated report to stdout.
func PrintReport(r FederatedReport) {
	sep := "--------------------------------------------------------------------------------"
	fmt.Println(sep)
	fmt.Println("FEDERATED EVALUATION REPORT")
	fmt.Println(sep)
	fmt.Printf("Generated:         %s\n", r.GeneratedAt.Format(time.RFC3339))
	fmt.Printf("Sites:             %d\n", len(r.Sites))
	fmt.Printf("Global cases:      %d\n", r.GlobalCases)
	fmt.Printf("Global accuracy:   %.1f%%\n", r.GlobalAccuracy*100)
	fmt.Println()

	// Per-site table
	fmt.Printf("  %-15s %-25s %8s %10s %10s %10s\n",
		"Site ID", "Site Name", "Cases", "Accuracy", "Latency", "Cost")
	fmt.Printf("  %s\n", "-------------------------------------------------------------------------------")
	for _, s := range r.Sites {
		fmt.Printf("  %-15s %-25s %8d %9.1f%% %8.0fms $%8.4f\n",
			s.SiteID, s.SiteName, s.TotalCases,
			s.Accuracy*100, s.AvgLatencyMs, s.AvgCostUSD)
	}
	fmt.Println()

	// Per-condition global
	if len(r.ByCondition) > 0 {
		fmt.Println("Global accuracy by condition:")
		for cond, acc := range r.ByCondition {
			fmt.Printf("  %-30s %.1f%%\n", cond, acc*100)
		}
		fmt.Println()
	}

	// Outliers
	if len(r.Outliers) > 0 {
		fmt.Println("OUTLIER SITES (> 2 std dev below mean):")
		for _, id := range r.Outliers {
			fmt.Printf("  - %s\n", id)
		}
	} else {
		fmt.Println("No outlier sites detected.")
	}
	fmt.Println(sep)
}
