package multiturneval

import "github.com/PratikDhanave/bodh/pkg/llm"

// ConversationTurn is one user message + expected assistant behavior in a multi-turn script.
type ConversationTurn struct {
	UserMessage string
	// CheckFn is optional — if set, called after this turn to check intermediate state.
	// Returns "" if OK, or an error string.
	CheckFn func(output string) string
}

// MultiTurnCase is one scripted multi-turn conversation with a final evaluator.
type MultiTurnCase struct {
	Name   string
	System string // system prompt override; empty = default
	Turns  []ConversationTurn
	// FinalEvaluators score the LAST assistant response.
	FinalEvaluators map[string]func(output string) float64
}

// TurnResult records what happened in one turn.
type TurnResult struct {
	Turn   int
	Input  string
	Output string
	Check  string // empty = passed
}

// MultiTurnResult records the full outcome for one MultiTurnCase.
type MultiTurnResult struct {
	Case        MultiTurnCase
	TurnResults []TurnResult
	FinalOutput string
	Scores      map[string]float64
	Err         error
}

// ConversationHistory is a helper that tracks llm.Message pairs.
type ConversationHistory struct {
	Messages []llm.Message
}
