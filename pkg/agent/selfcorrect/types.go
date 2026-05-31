// Package selfcorrect implements the "13-Self-Correction" lesson: the
// reflexion pattern for general-purpose agents. The agent produces an answer,
// critiques it, and revises based on the critique until the answer meets a
// quality threshold or the round limit is reached.
//
// This is the general-purpose tutorial analogue of the clinical
// ReflexionDiagnostician in agents/diagnostician_reflexion.go.
//
// The three-phase loop:
//
//	Answer → Critique → Revise → Critique → …
//
// The SelfCorrectCallbacks type provides optional lifecycle hooks for all
// phases. All fields are optional; nil hooks are ignored.
package selfcorrect

// CritiqueResult holds the structured output of one critique pass.
type CritiqueResult struct {
	// IsCorrect is true when the LLM considers the answer acceptable as-is.
	IsCorrect bool

	// Issues lists specific problems found in the answer.
	Issues []string

	// Suggestions lists how to fix each issue, in the same order as Issues.
	Suggestions []string

	// Score is a 0.0–1.0 quality score. 1.0 = perfect, 0.0 = completely wrong.
	Score float64
}

// RevisionResult is the final output of RunSelfCorrectAgent, carrying the
// complete history of the correction process.
type RevisionResult struct {
	// OriginalAnswer is the unmodified first draft from the agent.
	OriginalAnswer string

	// Critique is the last critique applied before the run ended.
	// If only one round ran (score >= threshold on first critique), this is
	// the critique of the original answer.
	Critique CritiqueResult

	// RevisedAnswer is the answer after all revision rounds. Equal to
	// OriginalAnswer when no revision was needed or possible.
	RevisedAnswer string

	// Improved is true when the final answer differs from the original and
	// the final critique score is higher than the first critique score.
	Improved bool

	// Rounds is the total number of critique rounds that ran (1 = only the
	// initial critique, 2 = one revision + its critique, …).
	Rounds int
}

// SelfCorrectCallbacks provides optional lifecycle hooks for a self-correction
// run. Callers use these for logging, streaming output, or metrics without
// modifying RunSelfCorrectAgent. All fields are optional; nil hooks are
// ignored.
type SelfCorrectCallbacks struct {
	// OnAnswer is called each time an answer is produced (initial draft or
	// revised). round=1 for the initial answer, 2+ for each revision.
	OnAnswer func(round int, answer string)

	// OnCritique is called after each critique pass.
	OnCritique func(round int, critique CritiqueResult)

	// OnRevision is called after each revised answer is produced.
	OnRevision func(round int, revised string)

	// OnDone is called once, with the final RevisionResult, immediately
	// before RunSelfCorrectAgent returns.
	OnDone func(result RevisionResult)
}
