// Package perturbation implements robustness testing for multi-turn
// conversations by mutating known-passing conversation traces.
//
// From "Evals for AI Engineers" Chapter 6: test agent robustness by mutating
// passing conversations and verifying the agent handles the perturbation
// gracefully.
//
// Supported perturbation types:
//   - GoalChange: change the user's goal mid-conversation
//   - AddAmbiguity: make a clear message vague by removing specifics
//   - Contradict: user disagrees with the agent's previous response
//   - AddConstraint: introduce a new requirement mid-stream
//   - DropContext: remove earlier conversational context
//   - RepeatRequest: ask the same question again
//
// Quick start:
//
//	trace := perturbation.ConversationTrace{
//	    ID: "case-001",
//	    Turns: []perturbation.Turn{
//	        {Role: "user", Content: "What causes diabetes?"},
//	        {Role: "assistant", Content: "Diabetes is caused by..."},
//	        {Role: "user", Content: "And what about treatment?"},
//	        {Role: "assistant", Content: "Treatment includes..."},
//	    },
//	}
//	perturbs := perturbation.GeneratePerturbations(trace)
//	results := perturbation.RunPerturbationSuite(
//	    []perturbation.ConversationTrace{trace},
//	    executor,
//	)
//	perturbation.PrintReport(results)
package perturbation

import (
	"context"
	"fmt"
	"strings"
)

// PerturbationType identifies the kind of mutation applied to a conversation
// turn.
type PerturbationType string

const (
	// PerturbGoalChange replaces the user's current goal with a completely
	// different one. Example: "Actually, forget about X. I need Y instead."
	PerturbGoalChange PerturbationType = "goal_change"

	// PerturbAddAmbiguity makes a specific user message vague by stripping
	// concrete details and leaving only approximate intent.
	PerturbAddAmbiguity PerturbationType = "add_ambiguity"

	// PerturbContradict inserts a user message that directly contradicts the
	// agent's previous assertion. Example: "No, that's not right."
	PerturbContradict PerturbationType = "contradict_agent"

	// PerturbAddConstraint introduces an additional requirement that the
	// conversation did not originally contain. Example: "Also, it must be
	// pet-friendly."
	PerturbAddConstraint PerturbationType = "add_constraint"

	// PerturbDropContext removes earlier conversational context so the agent
	// must work with an incomplete history.
	PerturbDropContext PerturbationType = "drop_context"

	// PerturbRepeatRequest re-sends the exact same user message that was
	// already sent in an earlier turn.
	PerturbRepeatRequest PerturbationType = "repeat_request"
)

// Perturbation describes a single mutation to a conversation trace.
type Perturbation struct {
	// Type identifies the perturbation strategy.
	Type PerturbationType

	// TurnIndex is the zero-based index of the turn that is mutated.
	TurnIndex int

	// Original is the user message at TurnIndex before mutation.
	Original string

	// Perturbed is the mutated message that replaces Original.
	Perturbed string

	// Description is a human-readable explanation of what this perturbation
	// does.
	Description string
}

// ConversationTrace is an ordered sequence of user and assistant turns.
type ConversationTrace struct {
	// ID uniquely identifies this trace (e.g. a case ID or scenario name).
	ID string

	// Turns contains the conversation history in chronological order.
	Turns []Turn
}

// Turn is a single message in a conversation.
type Turn struct {
	// Role is "user" or "assistant".
	Role string

	// Content is the message text.
	Content string
}

// PerturbationResult captures the outcome of running one perturbation against
// a conversation trace.
type PerturbationResult struct {
	// OriginalTrace is the unmodified conversation.
	OriginalTrace ConversationTrace

	// Perturbation is the mutation that was applied.
	Perturbation Perturbation

	// PerturbedTrace is the conversation after applying the perturbation and
	// collecting the agent's continuation.
	PerturbedTrace ConversationTrace

	// StillCorrect is true when the agent handled the perturbation
	// appropriately.
	StillCorrect bool

	// FailureMode describes what went wrong when StillCorrect is false.
	FailureMode string
}

// GeneratePerturbations produces a list of perturbations for every user turn
// in the trace. For each user turn, it generates one perturbation of each
// applicable type.
//
// Turns with role "assistant" are skipped. The ContradictAgent perturbation
// requires a preceding assistant turn, so it is only generated for user turns
// at index >= 2 (where the previous turn is an assistant turn).
func GeneratePerturbations(trace ConversationTrace) []Perturbation {
	var out []Perturbation

	for i, turn := range trace.Turns {
		if turn.Role != "user" {
			continue
		}

		// GoalChange: always applicable for user turns.
		out = append(out, GoalChangePerturbation(turn, i))

		// AddAmbiguity: always applicable for user turns.
		out = append(out, AmbiguityPerturbation(turn, i))

		// Contradict: requires a preceding assistant turn.
		if i >= 1 && trace.Turns[i-1].Role == "assistant" {
			out = append(out, ContradictionPerturbation(trace.Turns[i-1], turn, i))
		}

		// AddConstraint: always applicable for user turns.
		out = append(out, AddConstraintPerturbation(turn, i))

		// DropContext: only applicable if there are preceding user turns.
		if i >= 2 {
			out = append(out, DropContextPerturbation(turn, i))
		}

		// RepeatRequest: requires an earlier user turn to repeat.
		if i >= 2 {
			// Find the previous user turn to repeat.
			for j := i - 1; j >= 0; j-- {
				if trace.Turns[j].Role == "user" {
					out = append(out, RepeatRequestPerturbation(trace.Turns[j], i))
					break
				}
			}
		}
	}

	return out
}

// GoalChangePerturbation generates a perturbation that abandons the original
// goal and substitutes a different one.
//
// The perturbed message has the form:
//
//	"Actually, forget about <topic>. I need help with something else entirely."
func GoalChangePerturbation(turn Turn, turnIndex int) Perturbation {
	// Extract a rough topic from the original message.
	topic := extractTopic(turn.Content)
	perturbed := fmt.Sprintf(
		"Actually, forget about %s. I need help with something else entirely. "+
			"What are the symptoms of asthma?",
		topic,
	)

	return Perturbation{
		Type:      PerturbGoalChange,
		TurnIndex: turnIndex,
		Original:  turn.Content,
		Perturbed: perturbed,
		Description: fmt.Sprintf(
			"goal_change at turn %d: abandon %q and pivot to asthma symptoms",
			turnIndex, topic,
		),
	}
}

// AmbiguityPerturbation generates a perturbation that strips specific details
// from the user message, leaving only a vague request.
func AmbiguityPerturbation(turn Turn, turnIndex int) Perturbation {
	perturbed := makeAmbiguous(turn.Content)

	return Perturbation{
		Type:      PerturbAddAmbiguity,
		TurnIndex: turnIndex,
		Original:  turn.Content,
		Perturbed: perturbed,
		Description: fmt.Sprintf(
			"add_ambiguity at turn %d: stripped specifics from %q",
			turnIndex, truncate(turn.Content, 40),
		),
	}
}

// ContradictionPerturbation generates a perturbation where the user directly
// contradicts the agent's previous assertion.
func ContradictionPerturbation(prevAssistantTurn, userTurn Turn, turnIndex int) Perturbation {
	// Extract the first sentence of the assistant's response to reference.
	claim := firstSentence(prevAssistantTurn.Content)
	perturbed := fmt.Sprintf(
		"No, that's not right. I don't think %q is correct. Can you reconsider?",
		truncate(claim, 60),
	)

	return Perturbation{
		Type:      PerturbContradict,
		TurnIndex: turnIndex,
		Original:  userTurn.Content,
		Perturbed: perturbed,
		Description: fmt.Sprintf(
			"contradict_agent at turn %d: user disagrees with %q",
			turnIndex, truncate(claim, 40),
		),
	}
}

// AddConstraintPerturbation generates a perturbation that appends a new
// requirement to the user's existing message.
func AddConstraintPerturbation(turn Turn, turnIndex int) Perturbation {
	constraints := []string{
		"Also, it must be suitable for a pediatric patient.",
		"Also, the patient is allergic to penicillin.",
		"Also, the patient is pregnant.",
		"Also, the patient has renal impairment.",
		"Also, the patient is over 80 years old.",
	}
	// Select a constraint deterministically based on the turn index.
	constraint := constraints[turnIndex%len(constraints)]
	perturbed := turn.Content + " " + constraint

	return Perturbation{
		Type:      PerturbAddConstraint,
		TurnIndex: turnIndex,
		Original:  turn.Content,
		Perturbed: perturbed,
		Description: fmt.Sprintf(
			"add_constraint at turn %d: appended %q",
			turnIndex, constraint,
		),
	}
}

// DropContextPerturbation generates a perturbation that references earlier
// context without providing it, simulating a context window drop.
func DropContextPerturbation(turn Turn, turnIndex int) Perturbation {
	perturbed := "As I mentioned before, can you give me more details on that?"

	return Perturbation{
		Type:      PerturbDropContext,
		TurnIndex: turnIndex,
		Original:  turn.Content,
		Perturbed: perturbed,
		Description: fmt.Sprintf(
			"drop_context at turn %d: replaced specific message with vague back-reference",
			turnIndex,
		),
	}
}

// RepeatRequestPerturbation generates a perturbation that re-sends an earlier
// user message verbatim.
func RepeatRequestPerturbation(earlierTurn Turn, turnIndex int) Perturbation {
	return Perturbation{
		Type:      PerturbRepeatRequest,
		TurnIndex: turnIndex,
		Original:  earlierTurn.Content,
		Perturbed: earlierTurn.Content,
		Description: fmt.Sprintf(
			"repeat_request at turn %d: re-send earlier message %q",
			turnIndex, truncate(earlierTurn.Content, 40),
		),
	}
}

// ApplyPerturbation takes a conversation trace and a perturbation, and returns
// a new trace truncated at the perturbed turn. The user message at
// p.TurnIndex is replaced with p.Perturbed, and all subsequent turns are
// removed (since the agent must continue from the perturbed point).
//
// If p.TurnIndex is out of range, the original trace is returned unchanged.
func ApplyPerturbation(trace ConversationTrace, p Perturbation) ConversationTrace {
	if p.TurnIndex < 0 || p.TurnIndex >= len(trace.Turns) {
		return trace
	}

	// Copy turns up to and including TurnIndex.
	newTurns := make([]Turn, p.TurnIndex+1)
	copy(newTurns, trace.Turns[:p.TurnIndex+1])

	// Replace the turn at TurnIndex with the perturbed message.
	newTurns[p.TurnIndex] = Turn{
		Role:    "user",
		Content: p.Perturbed,
	}

	return ConversationTrace{
		ID:    trace.ID + "/perturbed",
		Turns: newTurns,
	}
}

// Executor is a function that takes context, a conversation history (turns so
// far), and returns the agent's next response and any error.
type Executor func(ctx context.Context, turns []Turn) (string, error)

// Checker is a function that validates whether the agent's response to a
// perturbation is acceptable. It returns (correct bool, failureMode string).
type Checker func(p Perturbation, response string) (bool, string)

// RunPerturbationSuite generates perturbations for every trace, applies each
// one, executes the agent, and checks the result using the provided checker.
//
// If executor is nil, it builds a PerturbedTrace with an empty continuation
// and marks StillCorrect as false with FailureMode "no executor provided".
//
// If checker is nil, a default checker is used that verifies the response is
// non-empty.
func RunPerturbationSuite(traces []ConversationTrace, executor Executor, checker Checker) []PerturbationResult {
	if checker == nil {
		checker = DefaultChecker
	}

	var results []PerturbationResult

	for _, trace := range traces {
		perturbs := GeneratePerturbations(trace)
		for _, p := range perturbs {
			result := PerturbationResult{
				OriginalTrace: trace,
				Perturbation:  p,
			}

			perturbed := ApplyPerturbation(trace, p)

			if executor == nil {
				result.PerturbedTrace = perturbed
				result.StillCorrect = false
				result.FailureMode = "no executor provided"
				results = append(results, result)
				continue
			}

			response, err := executor(context.Background(), perturbed.Turns)
			if err != nil {
				result.PerturbedTrace = perturbed
				result.StillCorrect = false
				result.FailureMode = fmt.Sprintf("executor error: %v", err)
				results = append(results, result)
				continue
			}

			// Append the agent response to the perturbed trace.
			perturbed.Turns = append(perturbed.Turns, Turn{
				Role:    "assistant",
				Content: response,
			})
			result.PerturbedTrace = perturbed

			ok, failMode := checker(p, response)
			result.StillCorrect = ok
			result.FailureMode = failMode
			results = append(results, result)
		}
	}

	return results
}

// DefaultChecker is a basic response checker that verifies the agent produced
// a non-empty response and applies type-specific heuristics:
//   - GoalChange: response should reference the new topic
//   - Contradict: response should acknowledge the disagreement
//   - All types: response must be non-empty
func DefaultChecker(p Perturbation, response string) (bool, string) {
	if strings.TrimSpace(response) == "" {
		return false, "empty response"
	}

	lower := strings.ToLower(response)

	switch p.Type {
	case PerturbGoalChange:
		// The default goal change pivots to asthma; check for that keyword.
		if !strings.Contains(lower, "asthma") {
			return false, "goal_change: response does not reference the new topic (asthma)"
		}
	case PerturbContradict:
		// The agent should acknowledge the disagreement in some way.
		acks := []string{"understand", "correct", "reconsider", "apolog", "you're right", "let me"}
		found := false
		for _, ack := range acks {
			if strings.Contains(lower, ack) {
				found = true
				break
			}
		}
		if !found {
			return false, "contradict: response does not acknowledge disagreement"
		}
	}

	return true, ""
}

// PrintReport writes a formatted summary of perturbation test results to
// stdout.
func PrintReport(results []PerturbationResult) {
	passed := 0
	failed := 0

	fmt.Println()
	fmt.Printf("%-15s  %-4s  %-6s  %s\n", "TYPE", "TURN", "STATUS", "FAILURE")
	fmt.Println(strings.Repeat("-", 90))

	for _, r := range results {
		status := "PASS"
		if !r.StillCorrect {
			status = "FAIL"
			failed++
		} else {
			passed++
		}

		fmt.Printf("%-15s  %-4d  %-6s  %s\n",
			r.Perturbation.Type,
			r.Perturbation.TurnIndex,
			status,
			r.FailureMode,
		)
	}

	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("Results: %d passed, %d failed, %d total\n", passed, failed, passed+failed)
	fmt.Println()
}

// --- internal helpers ---

// extractTopic attempts to pull a rough topic from a user message. It returns
// the first noun-phrase-like chunk after common question prefixes, or the
// first few words if no prefix is found.
func extractTopic(msg string) string {
	lower := strings.ToLower(msg)
	prefixes := []string{
		"what is ", "what are ", "tell me about ", "explain ",
		"how do ", "how does ", "what causes ", "what about ",
		"and ", "can you ",
	}
	for _, p := range prefixes {
		if idx := strings.Index(lower, p); idx >= 0 {
			rest := msg[idx+len(p):]
			rest = strings.TrimRight(rest, "?.! ")
			if len(rest) > 50 {
				rest = rest[:50]
			}
			return rest
		}
	}
	// Fallback: return up to the first 30 characters.
	s := strings.TrimRight(msg, "?.! ")
	if len(s) > 30 {
		s = s[:30]
	}
	return s
}

// makeAmbiguous strips specific medical terms and replaces them with vague
// language.
func makeAmbiguous(msg string) string {
	// If the message is short enough, just make it vague.
	if len(msg) < 20 {
		return "Can you tell me more about that?"
	}
	return "I have a question about that medical thing we discussed. Can you help?"
}

// firstSentence returns the text up to the first period, question mark, or
// exclamation mark. If none is found, it returns the whole string (truncated
// to 100 characters).
func firstSentence(s string) string {
	for i, ch := range s {
		if ch == '.' || ch == '?' || ch == '!' {
			return s[:i+1]
		}
	}
	if len(s) > 100 {
		return s[:100]
	}
	return s
}

// truncate returns s shortened to at most n characters, with "..." appended
// if truncation occurred.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
