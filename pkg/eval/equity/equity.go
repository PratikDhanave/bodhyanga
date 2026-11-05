// Package equity implements stratified accuracy measurement for the AIGP
// framework. It measures diagnostic accuracy broken down by demographic strata
// (age group, sex, race/ethnicity) to detect disparate performance across
// patient populations — a requirement for responsible clinical AI deployment.
package equity

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/eval"
)

// Stratum captures the demographic dimensions used for stratified analysis.
// Values are normalised to lower-case by ExtractStratum so comparisons are
// case-insensitive.
type Stratum struct {
	AgeGroup      string // "pediatric(<18)", "adult(18-64)", "elderly(65+)"
	Sex           string // "male", "female", "unknown"
	RaceEthnicity string // free text from case metadata
}

// StratifiedResult aggregates accuracy metrics for a single demographic bucket.
type StratifiedResult struct {
	Stratum   Stratum
	CaseCount int
	Correct   int
	Accuracy  float64
	P95CI     [2]float64 // 95% confidence interval (Wilson score)
}

// EquityReport is the top-level output of Stratify. It contains overall
// statistics plus per-dimension breakdowns and bias alerting.
type EquityReport struct {
	TotalCases      int
	OverallAccuracy float64
	ByAgeGroup      []StratifiedResult
	BySex           []StratifiedResult
	ByRace          []StratifiedResult
	// MaxDisparity is the maximum accuracy gap (absolute) between any two strata
	// within any single dimension. For example, if pediatric accuracy = 0.90
	// and elderly accuracy = 0.75, MaxDisparity = 0.15.
	MaxDisparity float64
	// BiasAlert is true when MaxDisparity > 0.10 (10 percentage points), the
	// threshold recommended by the AIGP framework for clinical AI systems.
	BiasAlert   bool
	GeneratedAt string
}

// caseEntry is an internal record pairing a stratum with its outcome.
type caseEntry struct {
	stratum Stratum
	correct bool
}

// ExtractStratum derives a Stratum from free-form case metadata. It reads the
// following keys:
//
//	"age"         – numeric age (int, float64, or similar numeric type)
//	"age_group"   – explicit age group string (overrides "age")
//	"sex"         – "male" / "female" / any string (stored lower-cased)
//	"gender"      – fallback when "sex" is absent
//	"race"        – race/ethnicity string
//	"ethnicity"   – appended to "race" when both present ("race / ethnicity")
func ExtractStratum(caseMetadata map[string]any) Stratum {
	s := Stratum{
		Sex:           "unknown",
		RaceEthnicity: "unknown",
		AgeGroup:      "unknown",
	}

	// --- age group ---
	if ag, ok := stringVal(caseMetadata, "age_group"); ok && ag != "" {
		s.AgeGroup = strings.ToLower(ag)
	} else if age, ok := numericVal(caseMetadata, "age"); ok {
		s.AgeGroup = ageGroupFromAge(age)
	}

	// --- sex / gender ---
	if sex, ok := stringVal(caseMetadata, "sex"); ok && sex != "" {
		s.Sex = strings.ToLower(sex)
	} else if gender, ok := stringVal(caseMetadata, "gender"); ok && gender != "" {
		s.Sex = strings.ToLower(gender)
	}

	// --- race / ethnicity ---
	race, hasRace := stringVal(caseMetadata, "race")
	eth, hasEth := stringVal(caseMetadata, "ethnicity")
	switch {
	case hasRace && hasEth && race != "" && eth != "":
		s.RaceEthnicity = strings.ToLower(race + " / " + eth)
	case hasRace && race != "":
		s.RaceEthnicity = strings.ToLower(race)
	case hasEth && eth != "":
		s.RaceEthnicity = strings.ToLower(eth)
	}

	return s
}

// Stratify groups a slice of CaseMetrics by demographic strata and returns a
// fully populated EquityReport. Each CaseMetrics.Metadata map should contain
// at least "correct" (bool or float64) plus any demographic keys supported by
// ExtractStratum.
//
// "Correct" is derived from the Metadata map key "correct" (bool), or
// "diagnosis_match" (bool), falling back to Accuracy >= 1.0 when neither is
// present.
func Stratify(results []eval.CaseMetrics) EquityReport {
	entries := make([]caseEntry, 0, len(results))
	totalCorrect := 0

	for _, r := range results {
		correct := extractCorrect(r)
		if correct {
			totalCorrect++
		}
		entries = append(entries, caseEntry{
			stratum: ExtractStratum(metadataOrEmpty(r)),
			correct: correct,
		})
	}

	n := len(entries)
	overall := 0.0
	if n > 0 {
		overall = float64(totalCorrect) / float64(n)
	}

	byAge := groupBy(entries,
		func(e caseEntry) string { return e.stratum.AgeGroup },
		func(e caseEntry) Stratum { return Stratum{AgeGroup: e.stratum.AgeGroup} })

	bySex := groupBy(entries,
		func(e caseEntry) string { return e.stratum.Sex },
		func(e caseEntry) Stratum { return Stratum{Sex: e.stratum.Sex} })

	byRace := groupBy(entries,
		func(e caseEntry) string { return e.stratum.RaceEthnicity },
		func(e caseEntry) Stratum { return Stratum{RaceEthnicity: e.stratum.RaceEthnicity} })

	maxDisp := maxDisparity(byAge, bySex, byRace)

	return EquityReport{
		TotalCases:      n,
		OverallAccuracy: overall,
		ByAgeGroup:      byAge,
		BySex:           bySex,
		ByRace:          byRace,
		MaxDisparity:    maxDisp,
		BiasAlert:       maxDisp > 0.10,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
	}
}

// WilsonCI computes the Wilson score interval for a proportion.
// correct is the number of successes, n is the total count, and z is the
// z-score for the desired confidence level (z=1.96 for 95%).
// Returns (lo, hi) clamped to [0, 1]. When n == 0 both bounds are 0.
func WilsonCI(correct, n int, z float64) (lo, hi float64) {
	if n == 0 {
		return 0, 0
	}
	p := float64(correct) / float64(n)
	nf := float64(n)
	z2 := z * z
	centre := p + z2/(2*nf)
	margin := z * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf))
	denom := 1 + z2/nf
	lo = math.Max(0, (centre-margin)/denom)
	hi = math.Min(1, (centre+margin)/denom)
	return lo, hi
}

// PrintEquityReport writes a human-readable equity report to stdout.
func PrintEquityReport(r EquityReport) {
	sep := strings.Repeat("-", 80)
	fmt.Println(sep)
	fmt.Println("EQUITY AUDIT REPORT")
	fmt.Println(sep)
	fmt.Printf("Generated:        %s\n", r.GeneratedAt)
	fmt.Printf("Total cases:      %d\n", r.TotalCases)
	fmt.Printf("Overall accuracy: %.1f%%\n", r.OverallAccuracy*100)
	fmt.Printf("Max disparity:    %.1f pp\n", r.MaxDisparity*100)
	if r.BiasAlert {
		fmt.Println("BIAS ALERT:       YES — disparity exceeds 10 percentage points")
	} else {
		fmt.Println("Bias alert:       no")
	}
	fmt.Println()

	printStratifiedTable("By Age Group", r.ByAgeGroup, func(s Stratum) string { return s.AgeGroup })
	printStratifiedTable("By Sex", r.BySex, func(s Stratum) string { return s.Sex })
	printStratifiedTable("By Race/Ethnicity", r.ByRace, func(s Stratum) string { return s.RaceEthnicity })
	fmt.Println(sep)
}

// -----------------------------------------------------------------------------
// internal helpers
// -----------------------------------------------------------------------------

// groupBy partitions entries by the string key returned by keyFn, then builds
// a StratifiedResult per bucket using the Stratum produced by stratumFn.
func groupBy(
	entries []caseEntry,
	keyFn func(caseEntry) string,
	stratumFn func(caseEntry) Stratum,
) []StratifiedResult {
	counts := make(map[string]int)
	corrects := make(map[string]int)
	strata := make(map[string]Stratum)

	for _, e := range entries {
		k := keyFn(e)
		counts[k]++
		if e.correct {
			corrects[k]++
		}
		strata[k] = stratumFn(e)
	}

	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]StratifiedResult, 0, len(keys))
	for _, k := range keys {
		n := counts[k]
		c := corrects[k]
		acc := 0.0
		if n > 0 {
			acc = float64(c) / float64(n)
		}
		lo, hi := WilsonCI(c, n, 1.96)
		out = append(out, StratifiedResult{
			Stratum:   strata[k],
			CaseCount: n,
			Correct:   c,
			Accuracy:  acc,
			P95CI:     [2]float64{lo, hi},
		})
	}
	return out
}

func maxDisparity(groups ...[]StratifiedResult) float64 {
	max := 0.0
	for _, g := range groups {
		d := disparityWithinGroup(g)
		if d > max {
			max = d
		}
	}
	return max
}

func disparityWithinGroup(g []StratifiedResult) float64 {
	if len(g) < 2 {
		return 0
	}
	minAcc := math.MaxFloat64
	maxAcc := -math.MaxFloat64
	for _, r := range g {
		if r.CaseCount == 0 {
			continue
		}
		if r.Accuracy < minAcc {
			minAcc = r.Accuracy
		}
		if r.Accuracy > maxAcc {
			maxAcc = r.Accuracy
		}
	}
	if minAcc == math.MaxFloat64 {
		return 0
	}
	return maxAcc - minAcc
}

func ageGroupFromAge(age float64) string {
	switch {
	case age < 18:
		return "pediatric(<18)"
	case age < 65:
		return "adult(18-64)"
	default:
		return "elderly(65+)"
	}
}

func extractCorrect(r eval.CaseMetrics) bool {
	// Prefer explicit bool keys in Metadata.
	if r.Metadata != nil {
		if v, ok := r.Metadata["correct"]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
		if v, ok := r.Metadata["diagnosis_match"]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	// Fall back to Accuracy field: 1.0 means correct.
	return r.Accuracy >= 1.0
}

func metadataOrEmpty(r eval.CaseMetrics) map[string]any {
	if r.Metadata != nil {
		return r.Metadata
	}
	return map[string]any{}
}

func stringVal(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func numericVal(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}

func printStratifiedTable(title string, results []StratifiedResult, labelFn func(Stratum) string) {
	fmt.Printf("%s\n", title)
	fmt.Printf("  %-30s %6s %8s %8s  %s\n", "Stratum", "N", "Correct", "Accuracy", "95% CI")
	fmt.Printf("  %s\n", strings.Repeat("-", 68))
	for _, r := range results {
		fmt.Printf("  %-30s %6d %8d %7.1f%%  [%.3f, %.3f]\n",
			labelFn(r.Stratum),
			r.CaseCount,
			r.Correct,
			r.Accuracy*100,
			r.P95CI[0],
			r.P95CI[1],
		)
	}
	fmt.Println()
}
