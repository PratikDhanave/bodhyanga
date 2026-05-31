// Package debate implements the "Multi-Agent Debate" lesson (Lesson 12).
//
// The debate pattern runs multiple specialized agents concurrently, each
// arguing from a distinct perspective, then a neutral chair agent synthesises
// the best answer. This teaches ensemble reasoning, concurrent LLM calls,
// and the value of adversarial collaboration.
package debate

// Debater represents one participant in the debate panel. Each debater has a
// unique perspective (system prompt) that shapes how it argues about the
// question. Examples: "Optimist", "Critic", "Pragmatist".
type Debater struct {
	// Name is a short label for this debater, e.g. "Pragmatist".
	Name string

	// Perspective is the system prompt describing this agent's viewpoint.
	// It frames how the debater should approach and argue the question.
	Perspective string
}

// Position is one debater's argued stance on the question.
type Position struct {
	// Debater is the Name of the Debater that produced this position.
	Debater string

	// Argument is the debater's argument, distilled from the LLM response.
	Argument string

	// Confidence is the debater's self-reported confidence in their argument,
	// as a value between 0.0 and 1.0.
	Confidence float64
}

// DebateResult is the final output of a debate run. It contains each
// debater's position and the chair's synthesised best answer.
type DebateResult struct {
	// Question is the original question posed to the debate panel.
	Question string

	// Positions holds each debater's individual argument.
	Positions []Position

	// Synthesis is the chair's synthesised best answer combining insights
	// from all debater positions.
	Synthesis string

	// Confidence is the chair's overall confidence in the synthesised answer,
	// between 0.0 and 1.0.
	Confidence float64
}

// DebateCallbacks provides optional lifecycle hooks for a debate run.
// Nil fields are ignored.
type DebateCallbacks struct {
	// OnPosition is called after each debater produces their argument.
	// debater is the Debater.Name; argument is the argued position.
	OnPosition func(debater string, argument string)

	// OnSynthesis is called when the chair produces the final synthesis.
	OnSynthesis func(result string)
}

// DefaultDebaters returns the 3-debater panel used when callers do not supply
// their own roster: Pragmatist (cost/speed), Architect (scalability), and
// Operator (reliability).
func DefaultDebaters() []Debater {
	return []Debater{
		{
			Name:        "Pragmatist",
			Perspective: "You are a pragmatic engineering lead focused on delivery speed and cost. You value simplicity, time-to-market, and minimising operational overhead. You are skeptical of over-engineering and prefer solutions that work today over theoretically perfect solutions for the future.",
		},
		{
			Name:        "Architect",
			Perspective: "You are a senior software architect focused on long-term scalability and system design. You value clear separation of concerns, independent deployability, and the ability to scale individual components. You think in terms of domain boundaries, team autonomy, and future growth.",
		},
		{
			Name:        "Operator",
			Perspective: "You are a site reliability engineer (SRE) focused on operational reliability, observability, and incident response. You value simplicity in deployment, clear failure modes, easy debugging, and minimal blast radius when things go wrong. You care deeply about on-call burden and production stability.",
		},
	}
}
