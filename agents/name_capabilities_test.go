package agents

import (
	"testing"

	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/tasks"
)

// TestAllAgentNameAndCapabilities exercises every agent's Name() and
// Capabilities() methods to ensure they return non-empty values. This covers
// the many 0%-coverage Name/Capabilities getters scattered across agents.
func TestAllAgentNameAndCapabilities(t *testing.T) {
	agents := []struct {
		name  string
		agent interface {
			Name() string
			Capabilities() []string
		}
	}{
		{"DiagnosticianAgent", &DiagnosticianAgent{}},
		{"IntakeAgent", &IntakeAgent{}},
		{"QuestionerAgent", &QuestionerAgent{}},
		{"TestPlannerAgent", &TestPlannerAgent{}},
		{"HumanReviewAgent", &HumanReviewAgent{Queue: hitl.NewQueue()}},
		{"ReasoningVerifierAgent", &ReasoningVerifierAgent{}},
		{"CDMPlannerAgent", &CDMPlannerAgent{}},
		{"PanelManagerAgent", &PanelManagerAgent{Registry: medical.NewPatientRegistry()}},
		{"PharmacistAgent", &PharmacistAgent{}},
		{"InboxTriageAgent", &InboxTriageAgent{Tasks: tasks.NewQueue()}},
		{"LLMDiagnostician", &LLMDiagnostician{Client: &llm.Mock{}}},
		{"DebateDiagnostician", &DebateDiagnostician{Base: &LLMDiagnostician{Client: &llm.Mock{}}}},
		{"LLMDiagnosticianVision", &LLMDiagnosticianVision{Base: &LLMDiagnostician{Client: &llm.Mock{}}}},
		{"ToolUsingDiagnostician", &ToolUsingDiagnostician{Base: &LLMDiagnostician{Client: &llm.Mock{}}}},
		{"ReflexionDiagnostician", &ReflexionDiagnostician{Base: &LLMDiagnostician{Client: &llm.Mock{}}}},
		{"CascadeDiagnostician", &CascadeDiagnostician{SmallTier: &LLMDiagnostician{Client: &llm.Mock{}}, LargeTier: &LLMDiagnostician{Client: &llm.Mock{}}}},
		{"LLMQuestioner", &LLMQuestioner{Client: &llm.Mock{}}},
		{"LLMInboxTriage", &LLMInboxTriage{Client: &llm.Mock{}}},
		{"LLMReasoningVerifier", &LLMReasoningVerifier{Client: &llm.Mock{}}},
	}

	for _, tc := range agents {
		t.Run(tc.name+"/Name", func(t *testing.T) {
			name := tc.agent.Name()
			if name == "" {
				t.Errorf("%s.Name() is empty", tc.name)
			}
		})
		t.Run(tc.name+"/Capabilities", func(t *testing.T) {
			caps := tc.agent.Capabilities()
			if len(caps) == 0 {
				t.Errorf("%s.Capabilities() is empty", tc.name)
			}
		})
	}
}
