// Package collaborative implements inter-annotator agreement metrics for
// evaluating AI system outputs. It provides Cohen's Kappa for pairwise
// agreement, Fleiss' Kappa for multi-annotator agreement, rubric management
// for structured evaluation criteria, and alignment session support.
//
// These tools are essential for building reliable human evaluation pipelines:
// before trusting human labels, you must verify that annotators agree on what
// "pass" and "fail" mean. Low agreement signals ambiguous rubrics or
// insufficient annotator training.
//
// Typical usage:
//
//	result := collaborative.CohensKappa(annotationsA, annotationsB, "diagnostic_accuracy")
//	fmt.Printf("Kappa: %.3f (%s)\n", result.CohensKappa, result.Interpretation)
//
//	fk := collaborative.FleissKappa(allAnnotations, "phi_safety")
//	fmt.Printf("Fleiss' Kappa: %.3f (%s)\n", fk, collaborative.InterpretKappa(fk))
package collaborative

import (
	"math"
	"time"
)

// Label represents a binary evaluation judgment. Evaluators label each trace
// as either "pass" or "fail" against a specific rubric criterion.
type Label string

const (
	// LabelPass indicates the output meets the rubric criterion.
	LabelPass Label = "pass"
	// LabelFail indicates the output does not meet the rubric criterion.
	LabelFail Label = "fail"
)

// Annotation records a single evaluator's judgment on one trace for one
// rubric criterion. Each annotation captures both the label and the reasoning
// behind it, which is critical for alignment sessions where disagreements are
// discussed.
type Annotation struct {
	TraceID     string    `json:"trace_id"`
	AnnotatorID string    `json:"annotator_id"`
	Criterion   string    `json:"criterion"`
	Label       Label     `json:"label"`
	Reasoning   string    `json:"reasoning"`
	Timestamp   time.Time `json:"timestamp"`
}

// AgreementResult holds the output of a pairwise inter-annotator agreement
// analysis for a single rubric criterion. It includes both the raw percent
// agreement and the chance-corrected Cohen's Kappa, along with any specific
// disagreements for review in alignment sessions.
type AgreementResult struct {
	Criterion        string         `json:"criterion"`
	AnnotatorA       string         `json:"annotator_a"`
	AnnotatorB       string         `json:"annotator_b"`
	PercentAgreement float64        `json:"percent_agreement"`
	CohensKappa      float64        `json:"cohens_kappa"`
	Interpretation   string         `json:"interpretation"`
	Disagreements    []Disagreement `json:"disagreements"`
}

// Disagreement captures a single trace where two annotators assigned
// different labels. The reasoning fields enable productive alignment
// discussions — the goal is not to determine who is "right" but to refine
// the rubric until both annotators would agree.
type Disagreement struct {
	TraceID string `json:"trace_id"`
	LabelA  Label  `json:"label_a"`
	LabelB  Label  `json:"label_b"`
	ReasonA string `json:"reason_a"`
	ReasonB string `json:"reason_b"`
}

// CohensKappa computes the pairwise inter-annotator agreement between two
// sets of annotations for a specific rubric criterion. It builds a 2x2
// contingency table from the matched trace IDs and computes both raw percent
// agreement and chance-corrected Cohen's Kappa.
//
// Only traces that appear in both annotation sets are considered. Annotations
// for other criteria are ignored. If no matching traces exist, the result
// will have zero values.
//
// Cohen's Kappa formula:
//
//	kappa = (Po - Pe) / (1 - Pe)
//
// where Po is observed agreement and Pe is expected agreement by chance.
func CohensKappa(annotationsA, annotationsB []Annotation, criterion string) AgreementResult {
	// Index annotations by trace ID, filtered to the target criterion.
	indexA := indexByCriterion(annotationsA, criterion)
	indexB := indexByCriterion(annotationsB, criterion)

	// Identify annotator IDs from the first matching annotation in each set.
	annotatorA := findAnnotatorID(annotationsA, criterion)
	annotatorB := findAnnotatorID(annotationsB, criterion)

	// Build the 2x2 contingency table:
	//
	//              B=pass  B=fail
	//   A=pass  [  a       b    ]
	//   A=fail  [  c       d    ]
	var a, b, c, d int
	for traceID, annA := range indexA {
		annB, ok := indexB[traceID]
		if !ok {
			continue
		}
		switch {
		case annA.Label == LabelPass && annB.Label == LabelPass:
			a++
		case annA.Label == LabelPass && annB.Label == LabelFail:
			b++
		case annA.Label == LabelFail && annB.Label == LabelPass:
			c++
		case annA.Label == LabelFail && annB.Label == LabelFail:
			d++
		}
	}

	n := a + b + c + d
	if n == 0 {
		return AgreementResult{
			Criterion:      criterion,
			AnnotatorA:     annotatorA,
			AnnotatorB:     annotatorB,
			Interpretation: InterpretKappa(0),
		}
	}

	// Observed agreement: fraction of items both annotators agreed on.
	po := float64(a+d) / float64(n)

	// Expected agreement by chance.
	nf := float64(n)
	pYesA := float64(a+b) / nf // P(A says pass)
	pYesB := float64(a+c) / nf // P(B says pass)
	pNoA := float64(c+d) / nf  // P(A says fail)
	pNoB := float64(b+d) / nf  // P(B says fail)
	pe := pYesA*pYesB + pNoA*pNoB

	kappa := 0.0
	if pe < 1.0 {
		kappa = (po - pe) / (1.0 - pe)
	}

	disagreements := FindDisagreements(annotationsA, annotationsB, criterion)

	return AgreementResult{
		Criterion:        criterion,
		AnnotatorA:       annotatorA,
		AnnotatorB:       annotatorB,
		PercentAgreement: po,
		CohensKappa:      kappa,
		Interpretation:   InterpretKappa(kappa),
		Disagreements:    disagreements,
	}
}

// FleissKappa computes Fleiss' Kappa for multiple annotators evaluating the
// same set of traces on a specific rubric criterion. The input is a slice of
// annotation sets, one per annotator.
//
// Fleiss' Kappa generalises Cohen's Kappa to any fixed number of raters. It
// measures agreement beyond chance for categorical ratings.
//
// Each annotation set should contain annotations from a single annotator. Only
// traces present in all annotation sets are included in the computation.
//
// Returns 0.0 when there are fewer than 2 annotators or no common traces.
func FleissKappa(annotations [][]Annotation, criterion string) float64 {
	if len(annotations) < 2 {
		return 0.0
	}

	// Index each annotator's annotations by trace ID.
	indices := make([]map[string]Annotation, len(annotations))
	for i, anns := range annotations {
		indices[i] = indexByCriterion(anns, criterion)
	}

	// Find trace IDs present in ALL annotation sets.
	commonTraces := findCommonTraces(indices)
	if len(commonTraces) == 0 {
		return 0.0
	}

	n := len(commonTraces) // number of subjects
	k := len(annotations)  // number of raters
	// Categories: pass=0, fail=1
	numCategories := 2

	// Build the rating matrix: nij[i][j] = number of raters who assigned
	// category j to subject i.
	nij := make([][]int, n)
	for i, traceID := range commonTraces {
		nij[i] = make([]int, numCategories)
		for r := 0; r < k; r++ {
			ann, ok := indices[r][traceID]
			if !ok {
				continue
			}
			if ann.Label == LabelPass {
				nij[i][0]++
			} else {
				nij[i][1]++
			}
		}
	}

	// Compute P_i for each subject: the extent to which raters agree on
	// subject i.
	//
	//   P_i = (1 / (k*(k-1))) * sum_j(nij^2 - k)
	pBar := 0.0
	kf := float64(k)
	nf := float64(n)

	for i := 0; i < n; i++ {
		sum := 0
		for j := 0; j < numCategories; j++ {
			sum += nij[i][j] * nij[i][j]
		}
		pi := (float64(sum) - kf) / (kf * (kf - 1))
		pBar += pi
	}
	pBar /= nf

	// Compute p_j: the proportion of all ratings that are category j.
	pj := make([]float64, numCategories)
	total := nf * kf
	for i := 0; i < n; i++ {
		for j := 0; j < numCategories; j++ {
			pj[j] += float64(nij[i][j])
		}
	}
	for j := 0; j < numCategories; j++ {
		pj[j] /= total
	}

	// Pe_bar = sum(pj^2)
	peBar := 0.0
	for j := 0; j < numCategories; j++ {
		peBar += pj[j] * pj[j]
	}

	if math.Abs(peBar-1.0) < 1e-10 {
		return 0.0
	}

	return (pBar - peBar) / (1.0 - peBar)
}

// InterpretKappa returns a textual interpretation of a kappa value using the
// Landis & Koch (1977) scale:
//
//	< 0.00  "poor"
//	0.00-0.20  "slight"
//	0.21-0.40  "fair"
//	0.41-0.60  "moderate"
//	0.61-0.80  "substantial"
//	0.81-1.00  "almost perfect"
func InterpretKappa(kappa float64) string {
	switch {
	case kappa < 0:
		return "poor"
	case kappa <= 0.20:
		return "slight"
	case kappa <= 0.40:
		return "fair"
	case kappa <= 0.60:
		return "moderate"
	case kappa <= 0.80:
		return "substantial"
	default:
		return "almost perfect"
	}
}

// FindDisagreements returns all traces where annotators A and B assigned
// different labels for the given criterion. Each disagreement includes the
// reasoning from both annotators to facilitate alignment discussions.
func FindDisagreements(a, b []Annotation, criterion string) []Disagreement {
	indexA := indexByCriterion(a, criterion)
	indexB := indexByCriterion(b, criterion)

	var disagreements []Disagreement
	for traceID, annA := range indexA {
		annB, ok := indexB[traceID]
		if !ok {
			continue
		}
		if annA.Label != annB.Label {
			disagreements = append(disagreements, Disagreement{
				TraceID: traceID,
				LabelA:  annA.Label,
				LabelB:  annB.Label,
				ReasonA: annA.Reasoning,
				ReasonB: annB.Reasoning,
			})
		}
	}
	return disagreements
}

// indexByCriterion filters annotations for a specific criterion and returns
// them indexed by trace ID. If multiple annotations exist for the same trace
// and criterion, the last one wins (assumed to be the most recent).
func indexByCriterion(annotations []Annotation, criterion string) map[string]Annotation {
	m := make(map[string]Annotation)
	for _, ann := range annotations {
		if ann.Criterion == criterion {
			m[ann.TraceID] = ann
		}
	}
	return m
}

// findAnnotatorID returns the annotator ID from the first annotation matching
// the given criterion. Returns "" if none found.
func findAnnotatorID(annotations []Annotation, criterion string) string {
	for _, ann := range annotations {
		if ann.Criterion == criterion {
			return ann.AnnotatorID
		}
	}
	return ""
}

// findCommonTraces returns the sorted list of trace IDs that appear in every
// one of the provided indices.
func findCommonTraces(indices []map[string]Annotation) []string {
	if len(indices) == 0 {
		return nil
	}

	// Start with keys from the first index.
	candidates := make(map[string]bool)
	for traceID := range indices[0] {
		candidates[traceID] = true
	}

	// Intersect with each subsequent index.
	for i := 1; i < len(indices); i++ {
		for traceID := range candidates {
			if _, ok := indices[i][traceID]; !ok {
				delete(candidates, traceID)
			}
		}
	}

	result := make([]string, 0, len(candidates))
	for traceID := range candidates {
		result = append(result, traceID)
	}

	// Sort for deterministic output.
	sortStrings(result)
	return result
}

// sortStrings sorts a string slice in place. We avoid importing "sort" for
// this single use — a simple insertion sort is fine for the typical small
// number of trace IDs.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
