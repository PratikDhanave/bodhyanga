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
}

// Aggregate computes the Summary for a set of case results.
func Aggregate(results []CaseResult) Summary {
	s := Summary{Provider: "rule", Cases: len(results)}
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
	}
	if s.Scored > 0 {
		s.AccuracyPct = 100 * float64(s.Matches) / float64(s.Scored)
	}
	return s
}

// WriteTable renders a human-readable per-case table plus the aggregate line.
// NOTE: there is no cost column — the pipeline emits no per-case cost, so the
// Pareto axis is a documented pipeline prerequisite (specs/006), not a fake zero.
func WriteTable(w io.Writer, results []CaseResult, s Summary) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "CASE\tGOLD\tPRODUCED\tSTATUS\tFORCED")
	for _, r := range results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%v\n", r.ID, r.Gold, r.Produced, r.Status, r.Forced)
	}
	tw.Flush()
	fmt.Fprintf(w, "\n%s: %d/%d scored matched — accuracy %.1f%%  (unscored=%d failures=%d)\n",
		s.Provider, s.Matches, s.Scored, s.AccuracyPct, s.Unscored, s.Failures)
}
