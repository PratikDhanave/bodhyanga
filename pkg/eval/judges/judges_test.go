package judges

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// types.go tests
// ---------------------------------------------------------------------------

func TestJudgeVerdict_Fields(t *testing.T) {
	v := JudgeVerdict{
		Reasoning: "Looks correct.",
		Answer:    "Pass",
	}
	if v.Answer != "Pass" {
		t.Fatalf("expected Answer=Pass, got %q", v.Answer)
	}
	if v.Reasoning == "" {
		t.Fatal("expected non-empty Reasoning")
	}
}

func TestJudgeAccuracy_AllFields(t *testing.T) {
	acc := JudgeAccuracy{TPR: 0.9, TNR: 0.85, FPR: 0.15, FNR: 0.1}
	if acc.TPR+acc.FNR != 1.0 {
		t.Fatalf("TPR+FNR should be 1.0, got %v", acc.TPR+acc.FNR)
	}
	if acc.TNR+acc.FPR != 1.0 {
		t.Fatalf("TNR+FPR should be 1.0, got %v", acc.TNR+acc.FPR)
	}
}

func TestSuccessRateEstimate_CIOrdering(t *testing.T) {
	e := SuccessRateEstimate{
		PointEstimate: 0.75,
		LowerCI:       0.65,
		UpperCI:       0.85,
		RawRate:       0.80,
		TPR:           0.9,
		TNR:           0.85,
		NumTraces:     100,
		NumBootstrap:  1000,
	}
	if e.LowerCI > e.PointEstimate {
		t.Fatalf("LowerCI (%v) should be <= PointEstimate (%v)", e.LowerCI, e.PointEstimate)
	}
	if e.UpperCI < e.PointEstimate {
		t.Fatalf("UpperCI (%v) should be >= PointEstimate (%v)", e.UpperCI, e.PointEstimate)
	}
}

func TestLabeledTrace_Fields(t *testing.T) {
	lt := LabeledTrace{
		TraceID:    "trace-001",
		Input:      "Patient with chest pain.",
		Output:     "Diagnosis: ACS",
		HumanLabel: true,
	}
	if !lt.HumanLabel {
		t.Fatal("expected HumanLabel=true")
	}
	if lt.TraceID != "trace-001" {
		t.Fatalf("TraceID: got %q want trace-001", lt.TraceID)
	}
}

func TestCalibratedJudge_FewShotExamples(t *testing.T) {
	j := CalibratedJudge{
		Name:      "test-judge",
		Criterion: "test criterion",
		Prompt:    "Evaluate this.",
		FewShot: []FewShotExample{
			{
				Input:   "input-1",
				Output:  "output-1",
				Verdict: JudgeVerdict{Reasoning: "good", Answer: "Pass"},
			},
			{
				Input:   "input-2",
				Output:  "output-2",
				Verdict: JudgeVerdict{Reasoning: "bad", Answer: "Fail"},
			},
		},
		ModelID: "claude-sonnet-4-6",
	}
	if len(j.FewShot) != 2 {
		t.Fatalf("expected 2 few-shot examples, got %d", len(j.FewShot))
	}
	if j.FewShot[0].Verdict.Answer != "Pass" {
		t.Fatal("first example should be Pass")
	}
	if j.FewShot[1].Verdict.Answer != "Fail" {
		t.Fatal("second example should be Fail")
	}
}

// ---------------------------------------------------------------------------
// calibration.go tests
// ---------------------------------------------------------------------------

func approxEqual(a, b, tol float64) bool {
	return math.Abs(a-b) < tol
}

func TestComputeRates_PerfectClassifier(t *testing.T) {
	labels := []bool{true, true, true, false, false}
	preds := []bool{true, true, true, false, false}
	tpr, tnr := computeRates(labels, preds)
	if tpr != 1.0 {
		t.Fatalf("perfect classifier TPR: got %v want 1.0", tpr)
	}
	if tnr != 1.0 {
		t.Fatalf("perfect classifier TNR: got %v want 1.0", tnr)
	}
}

func TestComputeRates_InvertedClassifier(t *testing.T) {
	labels := []bool{true, true, false, false}
	preds := []bool{false, false, true, true}
	tpr, tnr := computeRates(labels, preds)
	if tpr != 0.0 {
		t.Fatalf("inverted classifier TPR: got %v want 0.0", tpr)
	}
	if tnr != 0.0 {
		t.Fatalf("inverted classifier TNR: got %v want 0.0", tnr)
	}
}

func TestComputeRates_Imbalanced(t *testing.T) {
	// 4 positives, 2 negatives. Predict 3/4 positives correctly, 1/2 negatives correctly.
	labels := []bool{true, true, true, true, false, false}
	preds := []bool{true, true, true, false, false, true}
	tpr, tnr := computeRates(labels, preds)
	// TPR = 3/4 = 0.75
	if !approxEqual(tpr, 0.75, 0.001) {
		t.Fatalf("TPR: got %v want 0.75", tpr)
	}
	// TNR = 1/2 = 0.5
	if !approxEqual(tnr, 0.5, 0.001) {
		t.Fatalf("TNR: got %v want 0.5", tnr)
	}
}

func TestComputeRates_EmptyInput(t *testing.T) {
	tpr, tnr := computeRates(nil, nil)
	if tpr != 0 || tnr != 0 {
		t.Fatalf("empty input: got TPR=%v TNR=%v, want 0, 0", tpr, tnr)
	}
}

func TestObservedRate(t *testing.T) {
	cases := []struct {
		name string
		preds []bool
		want  float64
	}{
		{"all_true", []bool{true, true, true}, 1.0},
		{"all_false", []bool{false, false}, 0.0},
		{"mixed", []bool{true, false, true, false, true}, 0.6},
		{"empty", nil, 0.0},
		{"single_true", []bool{true}, 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := observedRate(tc.preds)
			if !approxEqual(got, tc.want, 0.001) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestRoganGladen_PerfectClassifier(t *testing.T) {
	// With perfect TPR=1, TNR=1, the correction should return the observed rate.
	got := roganGladen(0.7, 1.0, 1.0)
	if !approxEqual(got, 0.7, 0.001) {
		t.Fatalf("perfect classifier: got %v want 0.7", got)
	}
}

func TestRoganGladen_ImperfectClassifier(t *testing.T) {
	// TPR=0.9, TNR=0.8. Observed=0.75.
	// theta = (0.75 + 0.8 - 1) / (0.9 + 0.8 - 1) = 0.55 / 0.7 = 0.7857...
	got := roganGladen(0.75, 0.9, 0.8)
	want := 0.55 / 0.7
	if !approxEqual(got, want, 0.001) {
		t.Fatalf("imperfect: got %v want %v", got, want)
	}
}

func TestRoganGladen_UselessClassifier(t *testing.T) {
	// TPR + TNR = 1 means the classifier is no better than chance.
	// Should fall back to observed rate.
	got := roganGladen(0.6, 0.5, 0.5)
	if !approxEqual(got, 0.6, 0.001) {
		t.Fatalf("useless classifier fallback: got %v want 0.6", got)
	}
}

func TestRoganGladen_ClampsToZeroOne(t *testing.T) {
	// Edge case: correction could go negative with very low observed rate
	// and imperfect TNR.
	got := roganGladen(0.05, 0.9, 0.8)
	if got < 0 || got > 1 {
		t.Fatalf("expected clamped to [0,1], got %v", got)
	}
}

func TestEstimateSuccessRate_PerfectClassifier(t *testing.T) {
	testLabels := []bool{true, true, true, false, false}
	testPreds := []bool{true, true, true, false, false}
	unlabeled := []bool{true, true, false, true, false, true, false, true, true, true}

	est := EstimateSuccessRate(testLabels, testPreds, unlabeled, 1000)

	// With perfect classifier, point estimate should equal raw rate.
	if !approxEqual(est.PointEstimate, est.RawRate, 0.001) {
		t.Fatalf("perfect classifier: point=%v raw=%v should be equal", est.PointEstimate, est.RawRate)
	}
	if est.TPR != 1.0 || est.TNR != 1.0 {
		t.Fatalf("expected perfect TPR/TNR, got %v/%v", est.TPR, est.TNR)
	}
	if est.NumTraces != 10 {
		t.Fatalf("NumTraces: got %d want 10", est.NumTraces)
	}
	if est.NumBootstrap != 1000 {
		t.Fatalf("NumBootstrap: got %d want 1000", est.NumBootstrap)
	}
}

func TestEstimateSuccessRate_CIBrackets(t *testing.T) {
	testLabels := make([]bool, 100)
	testPreds := make([]bool, 100)
	for i := range 100 {
		testLabels[i] = i < 80
		testPreds[i] = i < 75 || (i >= 80 && i < 85)
	}

	unlabeled := make([]bool, 200)
	for i := range 200 {
		unlabeled[i] = i < 140
	}

	est := EstimateSuccessRate(testLabels, testPreds, unlabeled, 2000)

	if est.LowerCI > est.PointEstimate {
		t.Fatalf("LowerCI (%v) > PointEstimate (%v)", est.LowerCI, est.PointEstimate)
	}
	if est.UpperCI < est.PointEstimate {
		t.Fatalf("UpperCI (%v) < PointEstimate (%v)", est.UpperCI, est.PointEstimate)
	}
	if est.LowerCI >= est.UpperCI {
		t.Fatalf("CI should have width > 0: [%v, %v]", est.LowerCI, est.UpperCI)
	}
}

func TestEstimateSuccessRate_EmptyUnlabeled(t *testing.T) {
	testLabels := []bool{true, false}
	testPreds := []bool{true, false}

	est := EstimateSuccessRate(testLabels, testPreds, nil, 100)
	if est.NumTraces != 0 {
		t.Fatalf("NumTraces: got %d want 0", est.NumTraces)
	}
	// With no data, point estimate comes from roganGladen(0, 1, 1) = 0.
	if est.PointEstimate != 0 {
		t.Fatalf("empty unlabeled: point=%v want 0", est.PointEstimate)
	}
}

func TestCalibrateJudge_PerfectJudge(t *testing.T) {
	judge := &CalibratedJudge{Name: "test"}
	testSet := []LabeledTrace{
		{TraceID: "1", Input: "a", Output: "b", HumanLabel: true},
		{TraceID: "2", Input: "c", Output: "d", HumanLabel: true},
		{TraceID: "3", Input: "e", Output: "f", HumanLabel: false},
		{TraceID: "4", Input: "g", Output: "h", HumanLabel: false},
	}

	// Perfect judge: always matches human label.
	acc := CalibrateJudge(judge, testSet, func(input, output string) bool {
		for _, t := range testSet {
			if t.Input == input && t.Output == output {
				return t.HumanLabel
			}
		}
		return false
	})

	if acc.TPR != 1.0 {
		t.Fatalf("TPR: got %v want 1.0", acc.TPR)
	}
	if acc.TNR != 1.0 {
		t.Fatalf("TNR: got %v want 1.0", acc.TNR)
	}
	if acc.FPR != 0 {
		t.Fatalf("FPR: got %v want 0", acc.FPR)
	}
	if acc.FNR != 0 {
		t.Fatalf("FNR: got %v want 0", acc.FNR)
	}
	// Should also be stored on the judge.
	if judge.Accuracy.TPR != 1.0 {
		t.Fatalf("judge.Accuracy.TPR not updated: %v", judge.Accuracy.TPR)
	}
}

func TestCalibrateJudge_AlwaysPassJudge(t *testing.T) {
	judge := &CalibratedJudge{Name: "always-pass"}
	testSet := []LabeledTrace{
		{TraceID: "1", HumanLabel: true},
		{TraceID: "2", HumanLabel: true},
		{TraceID: "3", HumanLabel: false},
		{TraceID: "4", HumanLabel: false},
	}

	// Judge always says Pass.
	acc := CalibrateJudge(judge, testSet, func(_, _ string) bool { return true })

	// TPR should be 1.0 (catches all positives).
	if acc.TPR != 1.0 {
		t.Fatalf("TPR: got %v want 1.0", acc.TPR)
	}
	// TNR should be 0.0 (misses all negatives).
	if acc.TNR != 0.0 {
		t.Fatalf("TNR: got %v want 0.0", acc.TNR)
	}
	// FPR should be 1.0.
	if acc.FPR != 1.0 {
		t.Fatalf("FPR: got %v want 1.0", acc.FPR)
	}
}

func TestCalibrateJudge_AlwaysFailJudge(t *testing.T) {
	judge := &CalibratedJudge{Name: "always-fail"}
	testSet := []LabeledTrace{
		{TraceID: "1", HumanLabel: true},
		{TraceID: "2", HumanLabel: false},
	}

	acc := CalibrateJudge(judge, testSet, func(_, _ string) bool { return false })

	if acc.TPR != 0.0 {
		t.Fatalf("TPR: got %v want 0.0", acc.TPR)
	}
	if acc.TNR != 1.0 {
		t.Fatalf("TNR: got %v want 1.0", acc.TNR)
	}
	if acc.FNR != 1.0 {
		t.Fatalf("FNR: got %v want 1.0", acc.FNR)
	}
}

func TestCalibrateJudge_EmptyTestSet(t *testing.T) {
	judge := &CalibratedJudge{Name: "empty"}
	acc := CalibrateJudge(judge, nil, func(_, _ string) bool { return true })
	if acc.TPR != 0 || acc.TNR != 0 {
		t.Fatalf("empty test set: TPR=%v TNR=%v want 0, 0", acc.TPR, acc.TNR)
	}
}

func TestSortFloat64s(t *testing.T) {
	s := []float64{5.0, 1.0, 3.0, 2.0, 4.0}
	sortFloat64s(s)
	for i := 1; i < len(s); i++ {
		if s[i] < s[i-1] {
			t.Fatalf("not sorted at index %d: %v", i, s)
		}
	}
}

func TestSortFloat64s_Empty(t *testing.T) {
	sortFloat64s(nil)      // should not panic
	sortFloat64s([]float64{}) // should not panic
}

func TestSortFloat64s_SingleElement(t *testing.T) {
	s := []float64{42.0}
	sortFloat64s(s)
	if s[0] != 42.0 {
		t.Fatalf("single element: got %v", s[0])
	}
}

func TestSafeDivide(t *testing.T) {
	if safeDivide(10, 0) != 0 {
		t.Fatal("divide by zero should return 0")
	}
	if !approxEqual(safeDivide(3, 4), 0.75, 0.001) {
		t.Fatalf("3/4: got %v", safeDivide(3, 4))
	}
}

func TestClamp01(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{-0.5, 0},
		{0, 0},
		{0.5, 0.5},
		{1.0, 1.0},
		{1.5, 1.0},
	}
	for _, tc := range cases {
		got := clamp01(tc.in)
		if got != tc.want {
			t.Fatalf("clamp01(%v): got %v want %v", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// clinical.go tests
// ---------------------------------------------------------------------------

func TestDiagnosisAccuracyJudge_Structure(t *testing.T) {
	j := DiagnosisAccuracyJudge()
	if j.Name != "diagnosis-accuracy" {
		t.Fatalf("Name: %q", j.Name)
	}
	if j.Criterion == "" {
		t.Fatal("Criterion should not be empty")
	}
	if j.Prompt == "" {
		t.Fatal("Prompt should not be empty")
	}
	if len(j.FewShot) < 2 {
		t.Fatalf("expected at least 2 few-shot examples, got %d", len(j.FewShot))
	}
	if j.ModelID == "" {
		t.Fatal("ModelID should not be empty")
	}

	// Verify mixed outcomes.
	var passCount, failCount int
	for _, ex := range j.FewShot {
		switch ex.Verdict.Answer {
		case "Pass":
			passCount++
		case "Fail":
			failCount++
		default:
			t.Fatalf("unexpected answer: %q", ex.Verdict.Answer)
		}
	}
	if passCount == 0 || failCount == 0 {
		t.Fatalf("few-shot examples should include both Pass and Fail outcomes: pass=%d fail=%d", passCount, failCount)
	}
}

func TestPHISafetyJudge_Structure(t *testing.T) {
	j := PHISafetyJudge()
	if j.Name != "phi-safety" {
		t.Fatalf("Name: %q", j.Name)
	}
	if j.Criterion == "" {
		t.Fatal("Criterion should not be empty")
	}
	if j.Prompt == "" {
		t.Fatal("Prompt should not be empty")
	}
	if len(j.FewShot) < 2 {
		t.Fatalf("expected at least 2 few-shot examples, got %d", len(j.FewShot))
	}
	if j.ModelID == "" {
		t.Fatal("ModelID should not be empty")
	}

	var passCount, failCount int
	for _, ex := range j.FewShot {
		switch ex.Verdict.Answer {
		case "Pass":
			passCount++
		case "Fail":
			failCount++
		default:
			t.Fatalf("unexpected answer: %q", ex.Verdict.Answer)
		}
	}
	if passCount == 0 || failCount == 0 {
		t.Fatalf("few-shot examples should include both Pass and Fail: pass=%d fail=%d", passCount, failCount)
	}
}

func TestCarePlanCompletenessJudge_Structure(t *testing.T) {
	j := CarePlanCompletenessJudge()
	if j.Name != "care-plan-completeness" {
		t.Fatalf("Name: %q", j.Name)
	}
	if j.Criterion == "" {
		t.Fatal("Criterion should not be empty")
	}
	if j.Prompt == "" {
		t.Fatal("Prompt should not be empty")
	}
	if len(j.FewShot) < 2 {
		t.Fatalf("expected at least 2 few-shot examples, got %d", len(j.FewShot))
	}
	if j.ModelID == "" {
		t.Fatal("ModelID should not be empty")
	}

	var passCount, failCount int
	for _, ex := range j.FewShot {
		switch ex.Verdict.Answer {
		case "Pass":
			passCount++
		case "Fail":
			failCount++
		default:
			t.Fatalf("unexpected answer: %q", ex.Verdict.Answer)
		}
	}
	if passCount == 0 || failCount == 0 {
		t.Fatalf("few-shot examples should include both Pass and Fail: pass=%d fail=%d", passCount, failCount)
	}
}

func TestClinicalJudges_FewShotExamplesHaveContent(t *testing.T) {
	judges := []CalibratedJudge{
		DiagnosisAccuracyJudge(),
		PHISafetyJudge(),
		CarePlanCompletenessJudge(),
	}
	for _, j := range judges {
		for i, ex := range j.FewShot {
			if ex.Input == "" {
				t.Fatalf("%s: few-shot[%d] has empty Input", j.Name, i)
			}
			if ex.Output == "" {
				t.Fatalf("%s: few-shot[%d] has empty Output", j.Name, i)
			}
			if ex.Verdict.Reasoning == "" {
				t.Fatalf("%s: few-shot[%d] has empty Reasoning", j.Name, i)
			}
			if ex.Verdict.Answer != "Pass" && ex.Verdict.Answer != "Fail" {
				t.Fatalf("%s: few-shot[%d] has invalid Answer: %q", j.Name, i, ex.Verdict.Answer)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: EstimateSuccessRate with CalibrateJudge
// ---------------------------------------------------------------------------

func TestCalibrateAndEstimate_EndToEnd(t *testing.T) {
	// Build a test set where the "judge" is an imperfect classifier.
	testSet := []LabeledTrace{
		{TraceID: "1", Input: "a", Output: "pass-output", HumanLabel: true},
		{TraceID: "2", Input: "b", Output: "pass-output", HumanLabel: true},
		{TraceID: "3", Input: "c", Output: "pass-output", HumanLabel: true},
		{TraceID: "4", Input: "d", Output: "pass-output", HumanLabel: true},
		{TraceID: "5", Input: "e", Output: "fail-output", HumanLabel: true},  // judge will say fail (FN)
		{TraceID: "6", Input: "f", Output: "fail-output", HumanLabel: false},
		{TraceID: "7", Input: "g", Output: "fail-output", HumanLabel: false},
		{TraceID: "8", Input: "h", Output: "fail-output", HumanLabel: false},
		{TraceID: "9", Input: "i", Output: "pass-output", HumanLabel: false}, // judge will say pass (FP)
		{TraceID: "10", Input: "j", Output: "fail-output", HumanLabel: false},
	}

	// Simple judge: passes when output is "pass-output".
	judgeFn := func(_, output string) bool {
		return output == "pass-output"
	}

	judge := &CalibratedJudge{Name: "test-e2e"}
	acc := CalibrateJudge(judge, testSet, judgeFn)

	// Expected: TP=4, FN=1, TN=4, FP=1
	// TPR = 4/5 = 0.8, TNR = 4/5 = 0.8
	if !approxEqual(acc.TPR, 0.8, 0.001) {
		t.Fatalf("TPR: got %v want 0.8", acc.TPR)
	}
	if !approxEqual(acc.TNR, 0.8, 0.001) {
		t.Fatalf("TNR: got %v want 0.8", acc.TNR)
	}

	// Build test labels/preds arrays for EstimateSuccessRate.
	testLabels := make([]bool, len(testSet))
	testPreds := make([]bool, len(testSet))
	for i, tr := range testSet {
		testLabels[i] = tr.HumanLabel
		testPreds[i] = judgeFn(tr.Input, tr.Output)
	}

	// Unlabeled: 60% judged as pass.
	unlabeled := make([]bool, 100)
	for i := range 100 {
		unlabeled[i] = i < 60
	}

	est := EstimateSuccessRate(testLabels, testPreds, unlabeled, 2000)

	// Rogan-Gladen: theta = (0.6 + 0.8 - 1) / (0.8 + 0.8 - 1) = 0.4 / 0.6 = 0.6667
	want := 0.4 / 0.6
	if !approxEqual(est.PointEstimate, want, 0.01) {
		t.Fatalf("corrected estimate: got %v want ~%v", est.PointEstimate, want)
	}
	if !approxEqual(est.RawRate, 0.6, 0.001) {
		t.Fatalf("raw rate: got %v want 0.6", est.RawRate)
	}
	// CI should bracket the point estimate.
	if est.LowerCI > est.PointEstimate || est.UpperCI < est.PointEstimate {
		t.Fatalf("CI [%v, %v] does not bracket point %v", est.LowerCI, est.UpperCI, est.PointEstimate)
	}
}

// ---------------------------------------------------------------------------
// Deterministic seed test: bootstrap should be reproducible.
// ---------------------------------------------------------------------------

func TestEstimateSuccessRate_Deterministic(t *testing.T) {
	labels := []bool{true, true, false, false}
	preds := []bool{true, true, false, false}
	unlabeled := []bool{true, false, true, true, false}

	est1 := EstimateSuccessRate(labels, preds, unlabeled, 500)
	est2 := EstimateSuccessRate(labels, preds, unlabeled, 500)

	if est1.PointEstimate != est2.PointEstimate {
		t.Fatalf("point estimates differ across runs: %v vs %v", est1.PointEstimate, est2.PointEstimate)
	}
	if est1.LowerCI != est2.LowerCI || est1.UpperCI != est2.UpperCI {
		t.Fatalf("CIs differ: [%v,%v] vs [%v,%v]", est1.LowerCI, est1.UpperCI, est2.LowerCI, est2.UpperCI)
	}
}
