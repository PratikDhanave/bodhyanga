package agents

import (
	"context"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// QuestionerAgent implements MAI-DxO "Ask Question" — gathers history before testing.
type QuestionerAgent struct{}

func (a *QuestionerAgent) ID() string   { return "questioner" }
func (a *QuestionerAgent) Name() string { return "History & Symptom Questioner" }
func (a *QuestionerAgent) Capabilities() []string {
	return []string{"ask_question", "history_taking"}
}

func (a *QuestionerAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}
	if state.Answers == nil {
		state.Answers = make(map[string]string)
	}

	// Rule-based demo questions (replace with LLM in production).
	questions := []string{
		"Duration and character of cough?",
		"Any fever, chills, or night sweats?",
		"Smoking history or occupational exposures?",
		"Recent travel or sick contacts?",
	}
	for _, q := range questions {
		env.Logf("[questioner] ask: %s", q)
		state.QuestionsAsked = append(state.QuestionsAsked, q)
		// Simulated patient answers for demo sequential diagnosis.
		state.Answers[q] = simulatedAnswer(q, state.Presentation)
	}

	state.Hypotheses = []string{"community_acquired_pneumonia", "viral_upper_respiratory_infection", "pulmonary_embolism"}
	env.Logf("[questioner] %d questions answered; hypotheses: %v", len(state.QuestionsAsked), state.Hypotheses)

	out := agent.NewMessage(
		a.ID(),
		"diagnostic_supervisor",
		agent.RoleAgent,
		"questions_complete",
		medical.EncodeState(state),
		copyMeta(msg.Metadata),
	)
	return []agent.Message{out}, nil
}

func simulatedAnswer(question, presentation string) string {
	_ = presentation
	switch question {
	case "Duration and character of cough?":
		return "Productive cough for 5 days, yellow sputum"
	case "Any fever, chills, or night sweats?":
		return "Fever to 38.9°C for 3 days, no night sweats"
	case "Smoking history or occupational exposures?":
		return "Former smoker, 10 pack-years, quit 5 years ago"
	default:
		return "No significant findings"
	}
}
