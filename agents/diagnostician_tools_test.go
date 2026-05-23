package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/tools"
)

// newToolsTestState builds a CaseState + its encoded message body for the
// tool-using diagnostician tests. We reuse the same fixture every test so
// only the LLM script differs across them.
func newToolsTestState(t *testing.T) (agent.Message, *fakeEnv) {
	t.Helper()
	_, encoded := newCaseStateJSON(t)
	msg := agent.NewMessage("supervisor", "diagnostician", agent.RoleAgent, "case_state", encoded, map[string]any{"case_id": "tools-1"})
	return msg, newFakeEnv()
}

// defaultRegistry is the production-shape registry the agent uses when the
// factory wires it up. Tests against the registry use a custom one to assert
// against specific tools getting invoked.
func defaultRegistry() *tools.Registry {
	return tools.NewRegistry(tools.DrugInteraction{}, tools.DefaultRiskCalculator(), tools.FHIRQuery{})
}

// finalProposalResponse is a canned scriptedStep that returns a DiagnosisProposal
// JSON body — used as the terminating turn in tool-loop tests.
func finalProposalResponse() *llm.Response {
	return &llm.Response{Content: `{"diagnosis":"Acute pulmonary embolism","confidence":0.82,"differential":["PE","Pneumonia"],"rationale":"Wells PE score in high band with hypoxia and tachycardia."}`}
}

// toolCallResponse wraps a single ToolCall (name + JSON args) as the model's
// reply for a single round.
func toolCallResponse(name, args string) *llm.Response {
	return &llm.Response{
		ToolCalls: []llm.ToolCall{{Name: name, Arguments: []byte(args)}},
	}
}

func TestToolsDiagnostician_NoToolCallsBreaksImmediately(t *testing.T) {
	// Model returns a final proposal on the very first call — one round, no
	// tool invocation, proposal emitted to the verifier.
	client := newScriptedLLM(scriptedStep{Resp: finalProposalResponse()})
	d := &ToolUsingDiagnostician{
		Base:  &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
		Tools: defaultRegistry(),
	}

	msg, env := newToolsTestState(t)
	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" || out[0].Type != "diagnosis_proposal" {
		t.Fatalf("expected one diagnosis_proposal to reasoning_verifier, got %+v", out)
	}
	if len(client.Calls) != 1 {
		t.Fatalf("want 1 LLM call, got %d", len(client.Calls))
	}
	// Tools must have been advertised on the (only) call.
	if len(client.Calls[0].Tools) == 0 {
		t.Fatalf("expected Tools advertised on the request")
	}
}

func TestToolsDiagnostician_OneToolCallThenFinal(t *testing.T) {
	// First call → drug_interaction tool call; second call → final proposal.
	// We use a custom registry whose DrugInteraction is wrapped so we can
	// assert it was actually invoked, separate from the scripted LLM Calls.
	spy := &spyTool{inner: tools.DrugInteraction{}}
	reg := tools.NewRegistry(spy, tools.DefaultRiskCalculator(), tools.FHIRQuery{})

	client := newScriptedLLM(
		scriptedStep{Resp: toolCallResponse("drug_interaction", `{"drugs":["lisinopril","spironolactone"]}`)},
		scriptedStep{Resp: finalProposalResponse()},
	)
	d := &ToolUsingDiagnostician{
		Base:  &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
		Tools: reg,
	}

	msg, env := newToolsTestState(t)
	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("expected proposal emission, got %+v", out)
	}
	if spy.calls != 1 {
		t.Fatalf("DrugInteraction Invoke count=%d, want 1", spy.calls)
	}
	if len(client.Calls) != 2 {
		t.Fatalf("want 2 LLM calls (tool round + final), got %d", len(client.Calls))
	}
	// The second call must contain the tool-call marker and tool-result
	// marker appended to the message history so the LLM sees what happened.
	hist := client.Calls[1].Messages
	if len(hist) < 3 {
		t.Fatalf("second-call history len=%d, want >= 3 (user + asst-toolcall + user-toolresult)", len(hist))
	}
	if hist[1].Role != llm.RoleAssistant || !strings.Contains(hist[1].Content, "[tool_call:drug_interaction]") {
		t.Fatalf("expected assistant tool-call marker at hist[1], got %+v", hist[1])
	}
	if hist[2].Role != llm.RoleUser || !strings.Contains(hist[2].Content, "[tool_result:drug_interaction]") {
		t.Fatalf("expected user tool-result marker at hist[2], got %+v", hist[2])
	}
}

func TestToolsDiagnostician_BoundedByMaxToolRounds(t *testing.T) {
	// Three rounds of tool calls in a row, never producing a final proposal.
	// With MaxToolRounds=3 the agent should exhaust the cap and fall back.
	client := newScriptedLLM(
		scriptedStep{Resp: toolCallResponse("drug_interaction", `{"drugs":["warfarin","ibuprofen"]}`)},
		scriptedStep{Resp: toolCallResponse("drug_interaction", `{"drugs":["warfarin","ibuprofen"]}`)},
		scriptedStep{Resp: toolCallResponse("drug_interaction", `{"drugs":["warfarin","ibuprofen"]}`)},
	)
	d := &ToolUsingDiagnostician{
		Base:          &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
		Tools:         defaultRegistry(),
		MaxToolRounds: 3,
	}

	msg, env := newToolsTestState(t)
	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("expected fallback proposal emitted to verifier, got %+v", out)
	}
	if len(client.Calls) != 3 {
		t.Fatalf("want 3 LLM calls before bail-out, got %d", len(client.Calls))
	}
	if !containsLog(env, "falling back to rule-based") {
		t.Fatalf("expected fallback log, got %v", env.logs)
	}
}

func TestToolsDiagnostician_UnknownToolNameLogsAndContinues(t *testing.T) {
	// First call → unknown tool name; second call → final proposal.
	// Agent must log the miss, return a stub error result, and not abort.
	client := newScriptedLLM(
		scriptedStep{Resp: toolCallResponse("not_a_tool", `{}`)},
		scriptedStep{Resp: finalProposalResponse()},
	)
	d := &ToolUsingDiagnostician{
		Base:  &LLMDiagnostician{Client: client, Fallback: &DiagnosticianAgent{}},
		Tools: defaultRegistry(),
	}

	msg, env := newToolsTestState(t)
	out, err := d.HandleMessage(context.Background(), msg, env)
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 || out[0].To != "reasoning_verifier" {
		t.Fatalf("expected proposal emission, got %+v", out)
	}
	if !containsLog(env, "unknown tool") {
		t.Fatalf("expected unknown-tool log, got %v", env.logs)
	}
	// Second call must carry the stub error result so the model saw it.
	hist := client.Calls[1].Messages
	if len(hist) < 3 || !strings.Contains(hist[2].Content, `"error":"unknown tool"`) {
		t.Fatalf("expected stub error in tool-result turn, hist[2]=%q", hist[2].Content)
	}
}

// spyTool wraps another Tool and counts Invoke calls so tests can assert the
// agent actually executed the model's tool call.
type spyTool struct {
	inner tools.Tool
	calls int
}

func (s *spyTool) Name() string                 { return s.inner.Name() }
func (s *spyTool) Description() string          { return s.inner.Description() }
func (s *spyTool) Schema() json.RawMessage      { return s.inner.Schema() }
func (s *spyTool) Invoke(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	s.calls++
	return s.inner.Invoke(ctx, args)
}
