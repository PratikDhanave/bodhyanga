package bench

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// Summary is the aggregate over a suite run. Accuracy is matches ÷ scored,
// where scored = match + miss (unscored and failures are excluded from the
// denominator so a missing gold or a broken case can't inflate or deflate it).
type Summary struct {
	Provider    string  `json:"provider"`
	Cases       int     `json:"cases"`
	Scored      int     `json:"scored"`
	Matches     int     `json:"matches"`
	Unscored    int     `json:"unscored"`
	Failures    int     `json:"failures"`
	AccuracyPct float64 `json:"accuracy_pct"`
	// MeanCostUSD is the average test cost over completed cases (specs/006) —
	// the Pareto cost axis alongside accuracy. The pipeline now emits real
	// per-case cost, so this is no longer a flagged gap.
	MeanCostUSD float64 `json:"mean_cost_usd"`
}

// Aggregate computes the Summary for a set of case results.
func Aggregate(results []CaseResult) Summary {
	s := Summary{Provider: "rule", Cases: len(results)}
	var costSum float64
	var completed int
	for _, r := range results {
		switch r.Status {
		case StatusMatch:
			s.Scored++
			s.Matches++
		case StatusMiss:
			s.Scored++
		case StatusUnscored:
			s.Unscored++
		case StatusFailure:
			s.Failures++
		}
		// Cost exists for any case that produced a plan (everything but a
		// failure); mean over those.
		if r.Status != StatusFailure {
			costSum += r.CostUSD
			completed++
		}
	}
	if s.Scored > 0 {
		s.AccuracyPct = 100 * float64(s.Matches) / float64(s.Scored)
	}
	if completed > 0 {
		s.MeanCostUSD = costSum / float64(completed)
	}
	return s
}

// WriteTable renders a human-readable per-case table plus the Pareto aggregate
// line (accuracy AND mean cost — specs/006 closed the cost gap).
func WriteTable(w io.Writer, results []CaseResult, s Summary) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "CASE\tGOLD\tPRODUCED\tSTATUS\tFORCED\tCOST_USD")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%v\t%.0f\n", r.ID, r.Gold, r.Produced, r.Status, r.Forced, r.CostUSD)
	}
	tw.Flush()
	fmt.Fprintf(w, "\n%s: %d/%d scored matched — accuracy %.1f%%, mean cost $%.0f  (unscored=%d failures=%d)\n",
		s.Provider, s.Matches, s.Scored, s.AccuracyPct, s.MeanCostUSD, s.Unscored, s.Failures)
}
