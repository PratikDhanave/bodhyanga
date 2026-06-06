// Package judges provides clinical domain LLM-as-Judge evaluators with
// TPR/TNR calibration, implementing the patterns from "Evals for AI
// Engineers" Chapter 5.
//
// A CalibratedJudge wraps an LLM prompt that classifies a (input, output)
// pair as Pass or Fail. The calibration workflow measures TPR and TNR on a
// human-labeled test set, then uses the Rogan-Gladen estimator to correct
// the observed pass rate on unlabeled production traces.
package judges

import "time"

// JudgeVerdict is the structured output an LLM-as-Judge returns for one
// trace. Answer is always "Pass" or "Fail"; Reasoning captures the model's
// chain-of-thought so reviewers can audit disagreements.
type JudgeVerdict struct {
	Reasoning string `json:"reasoning"`
	Answer    string `json:"answer"` // "Pass" or "Fail"
}

// JudgeAccuracy holds the four rates from a confusion matrix comparing
// judge predictions to human labels.
type JudgeAccuracy struct {
	TPR float64 // true positive rate (Pass correctly identified)
	TNR float64 // true negative rate (Fail correctly identified)
	FPR float64 // false positive rate
	FNR float64 // false negative rate
}

// CalibratedJudge is a domain-specific LLM-as-Judge whose accuracy has
// been measured on a labeled test set. Pinning the ModelID and recording
// CalibratedAt makes it possible to detect when re-calibration is needed
// after a model update.
type CalibratedJudge struct {
	Name         string
	Criterion    string // what the judge evaluates, e.g. "diagnosis accuracy"
	Prompt       string // the LLM-as-Judge prompt template
	FewShot      []FewShotExample
	Accuracy     JudgeAccuracy // from test set calibration
	ModelID      string        // pinned model version e.g. "claude-sonnet-4-6"
	CalibratedAt time.Time
}

// FewShotExample is one labeled example appended to the judge prompt to
// steer the model toward the desired rubric. Including examples from
// different outcomes (Pass and Fail) reduces evaluation bias.
type FewShotExample struct {
	Input   string       `json:"input"`
	Output  string       `json:"output"`
	Verdict JudgeVerdict `json:"verdict"`
}

// SuccessRateEstimate is the result of applying the Rogan-Gladen
// estimator to correct an observed pass rate using the judge's known
// TPR and TNR. LowerCI and UpperCI are the 2.5th and 97.5th percentiles
// from bootstrap resampling.
type SuccessRateEstimate struct {
	PointEstimate float64 // corrected success rate
	LowerCI       float64 // 2.5th percentile
	UpperCI       float64 // 97.5th percentile
	RawRate       float64 // observed rate before correction
	TPR           float64
	TNR           float64
	NumTraces     int
	NumBootstrap  int
}

// LabeledTrace is a single human-labeled example used to calibrate a judge.
// HumanLabel true means the trace is a "Pass" case.
type LabeledTrace struct {
	TraceID    string
	Input      string
	Output     string
	HumanLabel bool // true = Pass
}
