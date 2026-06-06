package judges

import (
	"math"
	"math/rand/v2"
)

// EstimateSuccessRate applies the Rogan-Gladen correction to an observed
// pass rate from an imperfect binary classifier (the judge), then uses
// bootstrap resampling to compute a 95% confidence interval.
//
// From "Evals for AI Engineers" Chapter 5:
//
//	theta_corrected = (p_observed + TNR - 1) / (TPR + TNR - 1)
//
// testLabels and testPreds are the human labels and judge predictions on
// the labeled calibration set (used to compute TPR/TNR). unlabeledPreds
// are the judge predictions on the production traces whose true success
// rate we want to estimate. B is the number of bootstrap iterations
// (typically 1000-10000).
func EstimateSuccessRate(testLabels, testPreds []bool, unlabeledPreds []bool, B int) SuccessRateEstimate {
	tpr, tnr := computeRates(testLabels, testPreds)

	// Observed pass rate on unlabeled data.
	rawRate := observedRate(unlabeledPreds)

	// Rogan-Gladen point estimate.
	point := roganGladen(rawRate, tpr, tnr)

	// Bootstrap: resample unlabeled predictions B times.
	if B < 1 {
		B = 1000
	}
	estimates := make([]float64, B)
	n := len(unlabeledPreds)
	if n == 0 {
		return SuccessRateEstimate{
			PointEstimate: point,
			LowerCI:       point,
			UpperCI:       point,
			RawRate:       rawRate,
			TPR:           tpr,
			TNR:           tnr,
			NumTraces:     0,
			NumBootstrap:  B,
		}
	}

	rng := rand.New(rand.NewPCG(42, 0))
	for i := range B {
		// Resample with replacement.
		passCount := 0
		for range n {
			idx := rng.IntN(n)
			if unlabeledPreds[idx] {
				passCount++
			}
		}
		pObs := float64(passCount) / float64(n)
		estimates[i] = roganGladen(pObs, tpr, tnr)
	}

	// Sort for percentile extraction. Using insertion sort is fine for
	// typical B values (1000-10000).
	sortFloat64s(estimates)

	lo := int(math.Floor(0.025 * float64(B)))
	hi := int(math.Floor(0.975 * float64(B)))
	if hi >= B {
		hi = B - 1
	}

	return SuccessRateEstimate{
		PointEstimate: point,
		LowerCI:       estimates[lo],
		UpperCI:       estimates[hi],
		RawRate:       rawRate,
		TPR:           tpr,
		TNR:           tnr,
		NumTraces:     n,
		NumBootstrap:  B,
	}
}

// CalibrateJudge evaluates a judge against a set of human-labeled traces
// and returns its confusion-matrix accuracy. The caller must supply a
// judgeFn that runs the judge prompt and returns true for "Pass".
//
// This function does not call an LLM directly; it accepts a function so
// callers can inject their own LLM client or mock.
func CalibrateJudge(judge *CalibratedJudge, testSet []LabeledTrace, judgeFn func(input, output string) bool) JudgeAccuracy {
	var tp, tn, fp, fn int
	for _, t := range testSet {
		pred := judgeFn(t.Input, t.Output)
		switch {
		case t.HumanLabel && pred:
			tp++
		case t.HumanLabel && !pred:
			fn++
		case !t.HumanLabel && pred:
			fp++
		case !t.HumanLabel && !pred:
			tn++
		}
	}

	acc := JudgeAccuracy{
		TPR: safeDivide(float64(tp), float64(tp+fn)),
		TNR: safeDivide(float64(tn), float64(tn+fp)),
		FPR: safeDivide(float64(fp), float64(fp+tn)),
		FNR: safeDivide(float64(fn), float64(fn+tp)),
	}
	judge.Accuracy = acc
	return acc
}

// roganGladen implements the Rogan-Gladen prevalence correction:
//
//	theta = (p_obs + TNR - 1) / (TPR + TNR - 1)
//
// When TPR + TNR == 1 (a useless classifier) the denominator is zero;
// we fall back to the observed rate.
func roganGladen(pObs, tpr, tnr float64) float64 {
	denom := tpr + tnr - 1
	if math.Abs(denom) < 1e-12 {
		return pObs
	}
	theta := (pObs + tnr - 1) / denom
	return clamp01(theta)
}

// computeRates returns (TPR, TNR) from paired label/prediction slices.
func computeRates(labels, preds []bool) (tpr, tnr float64) {
	var tp, fn, tn, fp int
	n := len(labels)
	if len(preds) < n {
		n = len(preds)
	}
	for i := range n {
		switch {
		case labels[i] && preds[i]:
			tp++
		case labels[i] && !preds[i]:
			fn++
		case !labels[i] && !preds[i]:
			tn++
		case !labels[i] && preds[i]:
			fp++
		}
	}
	tpr = safeDivide(float64(tp), float64(tp+fn))
	tnr = safeDivide(float64(tn), float64(tn+fp))
	return tpr, tnr
}

// observedRate returns the fraction of true values in preds.
func observedRate(preds []bool) float64 {
	if len(preds) == 0 {
		return 0
	}
	count := 0
	for _, p := range preds {
		if p {
			count++
		}
	}
	return float64(count) / float64(len(preds))
}

// safeDivide returns num/denom, or 0 when denom is zero.
func safeDivide(num, denom float64) float64 {
	if denom == 0 {
		return 0
	}
	return num / denom
}

// clamp01 restricts v to [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// sortFloat64s sorts a float64 slice in ascending order using the
// standard library sort algorithm via a simple insertion sort (adequate
// for the ~1000-10000 element bootstrap slices).
func sortFloat64s(s []float64) {
	n := len(s)
	for i := 1; i < n; i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}
