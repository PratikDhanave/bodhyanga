package singleturneval

// EvalCase is one row in the evaluation dataset.
type EvalCase struct {
	Input    string         // user message sent to the agent
	Expected map[string]any // ground-truth values passed to evaluators
	Metadata map[string]any // optional labels for reporting
}

// EvaluatorFunc scores one agent output against the expected values.
// Returns a float64 score (typically 0.0–1.0).
type EvaluatorFunc func(output string, expected map[string]any) float64

// EvalResult records the outcome for one EvalCase.
type EvalResult struct {
	Case   EvalCase
	Output string
	Scores map[string]float64
	Err    error
}
