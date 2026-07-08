package planning_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent/planning"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/tools"
)

// mockSequentialClient returns a different response on each call, cycling
// through the provided slice. When calls exceed the slice length, the last
// entry is repeated.
type mockSequentialClient struct {
	responses []*llm.Response
	errors    []error
	callIndex int
	calls     []llm.Request
}

func (m *mockSequentialClient) Provider() string { return "mock" }
func (m *mockSequentialClient) Model() string    { return "mock-model" }

func (m *mockSequentialClient) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	m.calls = append(m.calls, req)
	i := m.callIndex
	if i >= len(m.responses) {
		i = len(m.responses) - 1
	}
	m.callIndex++

	if i < len(m.errors) && m.errors[i] != nil {
		return nil, m.errors[i]
	}
	resp := m.responses[i]
	if resp == nil {
		return &llm.Response{Provider: "mock", Model: "mock-model"}, nil
	}
	return resp, nil
}

// echoTool is a simple tool that echoes its "message" argument back as JSON.
type echoTool struct{ name string }

func (e echoTool) Name() string        { return e.name }
func (e echoTool) Description() string { return "Echo the message argument back." }
func (e echoTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`)
}
func (e echoTool) Invoke(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	out, _ := json.Marshal(map[string]string{"echoed": in.Message})
	return out, nil
}

// TestRunPlanningAgent_TwoStepPlan verifies that RunPlanningAgent correctly
// executes a 2-step plan produced by a mock LLM and returns the final answer.
func TestRunPlanningAgent_TwoStepPlan(t *testing.T) {
	// --- Mock LLM responses -----------------------------------------------
	// Call 1: initial plan with 2 steps.
	planResponse := `{
  "goal": "echo hello then echo world",
  "steps": [
    {
      "id": "step-1",
      "description": "Echo hello",
      "tool_name": "echo",
      "tool_args": {"message": "hello"}
    },
    {
      "id": "step-2",
      "description": "Echo world",
      "tool_name": "echo",
      "tool_args": {"message": "world"}
    }
  ]
}`

	// Call 2: decision after step-1 → continue.
	decisionContinue := `{"action":"continue","reason":"step 1 succeeded, continue to step 2"}`

	// Call 3: decision after step-2 → done.
	decisionDone := `{"action":"done","result_if_done":"Both echoes completed: hello and world.","reason":"all steps done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}

	// --- Tool registry with the echo tool ---------------------------------
	reg := tools.NewRegistry(echoTool{name: "echo"})

	// --- Collect callback events ------------------------------------------
	var planCreated bool
	var stepsStarted []int
	var stepsCompleted []int
	var planCompleted bool
	var finalResult string

	cbs := planning.PlanningCallbacks{
		OnPlanCreated: func(p planning.Plan) {
			planCreated = true
			if p.Goal == "" {
				t.Error("OnPlanCreated: plan.Goal is empty")
			}
			if len(p.Steps) != 2 {
				t.Errorf("OnPlanCreated: expected 2 steps, got %d", len(p.Steps))
			}
		},
		OnStepStart: func(s planning.Step, index int) {
			stepsStarted = append(stepsStarted, index)
		},
		OnStepComplete: func(s planning.Step, index int, result string) {
			stepsCompleted = append(stepsCompleted, index)
			if s.Status != "done" {
				t.Errorf("OnStepComplete: step %d status=%q, want 'done'", index, s.Status)
			}
		},
		OnPlanComplete: func(result string) {
			planCompleted = true
			finalResult = result
		},
	}

	// --- Run ----------------------------------------------------------------
	ctx := context.Background()
	result, err := planning.RunPlanningAgent(ctx, client, "echo hello then echo world", reg, cbs, 10)
	if err != nil {
		t.Fatalf("RunPlanningAgent returned error: %v", err)
	}

	// --- Assertions ---------------------------------------------------------
	if !planCreated {
		t.Error("OnPlanCreated was not called")
	}
	if len(stepsStarted) != 2 {
		t.Errorf("expected 2 OnStepStart calls, got %d", len(stepsStarted))
	}
	if len(stepsCompleted) != 2 {
		t.Errorf("expected 2 OnStepComplete calls, got %d", len(stepsCompleted))
	}
	if !planCompleted {
		t.Error("OnPlanComplete was not called")
	}
	if result == "" {
		t.Error("RunPlanningAgent returned empty result")
	}
	if finalResult != result {
		t.Errorf("callback result %q != return value %q", finalResult, result)
	}
	// The mock LLM set result_if_done to this value.
	wantResult := "Both echoes completed: hello and world."
	if result != wantResult {
		t.Errorf("result = %q, want %q", result, wantResult)
	}

	// The LLM should have been called exactly 3 times:
	// 1. initial plan, 2. decision after step-1, 3. decision after step-2.
	if len(client.calls) != 3 {
		t.Errorf("expected 3 LLM calls, got %d", len(client.calls))
	}
}

// TestRunPlanningAgent_Replan verifies that the agent correctly handles a
// replan response by switching to the revised step list.
func TestRunPlanningAgent_Replan(t *testing.T) {
	// Initial plan: 2 steps.
	planResponse := `{
  "goal": "test replan",
  "steps": [
    {
      "id": "step-1",
      "description": "Echo first",
      "tool_name": "echo",
      "tool_args": {"message": "first"}
    },
    {
      "id": "step-2",
      "description": "Echo second (will be replaced by replan)",
      "tool_name": "echo",
      "tool_args": {"message": "second"}
    }
  ]
}`

	// After step-1: replan with a single replacement step.
	decisionReplan := `{
  "action": "replan",
  "reason": "discovered we only need one more step",
  "revised_steps_if_replan": [
    {
      "id": "step-2b",
      "description": "Echo revised",
      "tool_name": "echo",
      "tool_args": {"message": "revised"}
    }
  ]
}`

	// After step-2b (the revised step): done.
	decisionDone := `{"action":"done","result_if_done":"Replan succeeded: revised.","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionReplan, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}

	reg := tools.NewRegistry(echoTool{name: "echo"})

	var replanCalled bool
	cbs := planning.PlanningCallbacks{
		OnReplan: func(reason string, newPlan planning.Plan) {
			replanCalled = true
			if reason == "" {
				t.Error("OnReplan: reason is empty")
			}
		},
	}

	ctx := context.Background()
	result, err := planning.RunPlanningAgent(ctx, client, "test replan", reg, cbs, 10)
	if err != nil {
		t.Fatalf("RunPlanningAgent returned error: %v", err)
	}

	if !replanCalled {
		t.Error("OnReplan was not called")
	}
	wantResult := "Replan succeeded: revised."
	if result != wantResult {
		t.Errorf("result = %q, want %q", result, wantResult)
	}
}

// TestRunPlanningAgent_MaxStepsExceeded verifies that the max-steps guard fires.
func TestRunPlanningAgent_MaxStepsExceeded(t *testing.T) {
	// Plan with 3 steps.
	planResponse := `{
  "goal": "exceed max steps",
  "steps": [
    {"id":"s1","description":"step 1","tool_name":"echo","tool_args":{"message":"a"}},
    {"id":"s2","description":"step 2","tool_name":"echo","tool_args":{"message":"b"}},
    {"id":"s3","description":"step 3","tool_name":"echo","tool_args":{"message":"c"}}
  ]
}`

	// Decision always says continue.
	decisionContinue := `{"action":"continue","reason":"keep going"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
		},
	}

	reg := tools.NewRegistry(echoTool{name: "echo"})

	// maxSteps=2 means only 2 steps will execute before we get an error.
	_, err := planning.RunPlanningAgent(context.Background(), client, "exceed max steps", reg, planning.PlanningCallbacks{}, 2)
	if err == nil {
		t.Error("expected an error when maxSteps is exceeded, got nil")
	}
}

// TestExtractJSON_MixedContent is a white-box test for the JSON extraction
// helper used internally. We call it indirectly by supplying a plan response
// wrapped in markdown code fences and verifying the agent parses it correctly.
func TestExtractJSON_MarkdownFenced(t *testing.T) {
	planResponse := "```json\n" + `{
  "goal": "test markdown fencing",
  "steps": [
    {
      "id": "step-1",
      "description": "Echo fenced",
      "tool_name": "echo",
      "tool_args": {"message": "fenced"}
    }
  ]
}` + "\n```"

	decisionDone := `{"action":"done","result_if_done":"fenced response parsed ok","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}

	reg := tools.NewRegistry(echoTool{name: "echo"})

	result, err := planning.RunPlanningAgent(context.Background(), client, "test markdown fencing", reg, planning.PlanningCallbacks{}, 10)
	if err != nil {
		t.Fatalf("RunPlanningAgent with markdown-fenced plan returned error: %v", err)
	}
	if result != "fenced response parsed ok" {
		t.Errorf("result = %q, want %q", result, "fenced response parsed ok")
	}
}

// ---------------------------------------------------------------------------
// failingTool is a tool whose Invoke always returns an error.
// ---------------------------------------------------------------------------
type failingTool struct{ name string }

func (f failingTool) Name() string            { return f.name }
func (f failingTool) Description() string     { return "Always fails." }
func (f failingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f failingTool) Invoke(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("synthetic tool failure")
}

// ---------------------------------------------------------------------------
// Edge-case tests
// ---------------------------------------------------------------------------

// TestRunPlanningAgent_InitialPlanCallFails covers the case where the very
// first LLM call (plan creation) returns an error.
func TestRunPlanningAgent_InitialPlanCallFails(t *testing.T) {
	client := &mockSequentialClient{
		responses: []*llm.Response{nil},
		errors:    []error{fmt.Errorf("LLM unavailable")},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	_, err := planning.RunPlanningAgent(context.Background(), client, "some goal", reg, planning.PlanningCallbacks{}, 5)
	if err == nil {
		t.Fatal("expected error when initial plan call fails, got nil")
	}
	if !strings.Contains(err.Error(), "initial plan call") {
		t.Errorf("error should mention 'initial plan call', got: %v", err)
	}
}

// TestRunPlanningAgent_InvalidPlanJSON covers a plan response that is not
// valid JSON at all.
func TestRunPlanningAgent_InvalidPlanJSON(t *testing.T) {
	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: "this is not json at all", Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	_, err := planning.RunPlanningAgent(context.Background(), client, "bad json goal", reg, planning.PlanningCallbacks{}, 5)
	if err == nil {
		t.Fatal("expected error when plan JSON is invalid, got nil")
	}
	if !strings.Contains(err.Error(), "parse plan JSON") {
		t.Errorf("error should mention 'parse plan JSON', got: %v", err)
	}
}

// TestRunPlanningAgent_EmptyPlanSteps covers the case where the LLM returns
// a plan with zero steps — the agent should go straight to the final summary.
func TestRunPlanningAgent_EmptyPlanSteps(t *testing.T) {
	planResponse := `{"goal":"nothing to do","steps":[]}`
	summaryResponse := "No steps were needed."

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			// Final summary call (steps exhausted immediately).
			{Content: summaryResponse, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	var planCompleted bool
	cbs := planning.PlanningCallbacks{
		OnPlanComplete: func(result string) {
			planCompleted = true
		},
	}

	result, err := planning.RunPlanningAgent(context.Background(), client, "nothing to do", reg, cbs, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != summaryResponse {
		t.Errorf("result = %q, want %q", result, summaryResponse)
	}
	if !planCompleted {
		t.Error("OnPlanComplete should have been called for empty plan via final summary")
	}
}

// TestRunPlanningAgent_ContextCancelled covers context cancellation propagating
// through the initial LLM call.
func TestRunPlanningAgent_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	client := &mockSequentialClient{
		responses: []*llm.Response{nil},
		errors:    []error{ctx.Err()},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	_, err := planning.RunPlanningAgent(ctx, client, "cancelled goal", reg, planning.PlanningCallbacks{}, 5)
	if err == nil {
		t.Fatal("expected error when context is cancelled, got nil")
	}
}

// TestRunPlanningAgent_ToolNotFound covers a plan step referencing a tool that
// does not exist in the registry.
func TestRunPlanningAgent_ToolNotFound(t *testing.T) {
	planResponse := `{
  "goal": "use nonexistent tool",
  "steps": [
    {"id":"step-1","description":"call missing","tool_name":"nonexistent","tool_args":{}}
  ]
}`
	// After step-1 fails, the decision says done.
	decisionDone := `{"action":"done","result_if_done":"handled missing tool","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	var stepStatus string
	cbs := planning.PlanningCallbacks{
		OnStepComplete: func(s planning.Step, index int, result string) {
			stepStatus = s.Status
		},
	}

	result, err := planning.RunPlanningAgent(context.Background(), client, "use nonexistent tool", reg, cbs, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stepStatus != "failed" {
		t.Errorf("step status should be 'failed' for missing tool, got %q", stepStatus)
	}
	if result != "handled missing tool" {
		t.Errorf("result = %q, want %q", result, "handled missing tool")
	}
}

// TestRunPlanningAgent_ToolInvocationFails covers a tool that returns an error
// from Invoke, exercising the failed step path including the FAILED summary
// in the decision context.
func TestRunPlanningAgent_ToolInvocationFails(t *testing.T) {
	planResponse := `{
  "goal": "tool will fail",
  "steps": [
    {"id":"step-1","description":"call failing tool","tool_name":"fail","tool_args":{}}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"failure handled","reason":"step failed"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(failingTool{name: "fail"})

	var completedStep planning.Step
	cbs := planning.PlanningCallbacks{
		OnStepComplete: func(s planning.Step, index int, result string) {
			completedStep = s
		},
	}

	result, err := planning.RunPlanningAgent(context.Background(), client, "tool will fail", reg, cbs, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if completedStep.Status != "failed" {
		t.Errorf("step status = %q, want 'failed'", completedStep.Status)
	}
	if completedStep.Error == "" {
		t.Error("step error should be set for a failing tool")
	}
	if result != "failure handled" {
		t.Errorf("result = %q, want %q", result, "failure handled")
	}
}

// TestRunPlanningAgent_DecisionCallFails covers the LLM decision call returning
// an error after a step completes.
func TestRunPlanningAgent_DecisionCallFails(t *testing.T) {
	planResponse := `{
  "goal": "decision will fail",
  "steps": [
    {"id":"step-1","description":"echo ok","tool_name":"echo","tool_args":{"message":"hello"}}
  ]
}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			nil, // decision call fails
		},
		errors: []error{nil, fmt.Errorf("decision LLM error")},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	_, err := planning.RunPlanningAgent(context.Background(), client, "decision will fail", reg, planning.PlanningCallbacks{}, 5)
	if err == nil {
		t.Fatal("expected error when decision call fails, got nil")
	}
	if !strings.Contains(err.Error(), "decision call after step") {
		t.Errorf("error should mention 'decision call after step', got: %v", err)
	}
}

// TestRunPlanningAgent_UnparseableDecisionDefaultsToContinue covers the case
// where the decision response is not valid JSON, so the agent defaults to
// "continue". The plan runs out of steps and falls through to the final summary.
func TestRunPlanningAgent_UnparseableDecisionDefaultsToContinue(t *testing.T) {
	planResponse := `{
  "goal": "unparseable decision",
  "steps": [
    {"id":"step-1","description":"echo once","tool_name":"echo","tool_args":{"message":"hi"}}
  ]
}`
	// Decision response is not JSON — agent should default to "continue".
	badDecision := "I cannot decide; this is free text."
	// After continuing, Current advances past the only step.
	// The agent then calls the final summary.
	summaryResponse := "Task completed despite bad decision."

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: badDecision, Provider: "mock", Model: "mock-model"},
			{Content: summaryResponse, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	result, err := planning.RunPlanningAgent(context.Background(), client, "unparseable decision", reg, planning.PlanningCallbacks{}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != summaryResponse {
		t.Errorf("result = %q, want %q", result, summaryResponse)
	}
}

// TestRunPlanningAgent_DefaultMaxSteps covers maxSteps <= 0 being replaced
// by DefaultMaxSteps (20).
func TestRunPlanningAgent_DefaultMaxSteps(t *testing.T) {
	// One-step plan that finishes immediately.
	planResponse := `{
  "goal": "quick finish",
  "steps": [
    {"id":"step-1","description":"echo","tool_name":"echo","tool_args":{"message":"a"}}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"done with default max steps","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	// Pass 0 as maxSteps — should default to DefaultMaxSteps (20).
	result, err := planning.RunPlanningAgent(context.Background(), client, "quick finish", reg, planning.PlanningCallbacks{}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done with default max steps" {
		t.Errorf("result = %q, want %q", result, "done with default max steps")
	}
}

// TestRunPlanningAgent_StepsWithMissingIDAndNilArgs covers step JSON where
// the id field is empty and tool_args is null — the agent should generate
// IDs and use empty arg maps.
func TestRunPlanningAgent_StepsWithMissingIDAndNilArgs(t *testing.T) {
	planResponse := `{
  "goal": "missing ids",
  "steps": [
    {"description":"echo default","tool_name":"echo","tool_args":null},
    {"id":"","description":"echo again","tool_name":"echo"}
  ]
}`
	decisionContinue := `{"action":"continue","reason":"next"}`
	decisionDone := `{"action":"done","result_if_done":"all done","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	// echoTool tolerates missing "message" — it just echoes empty.
	reg := tools.NewRegistry(echoTool{name: "echo"})

	var createdPlan planning.Plan
	cbs := planning.PlanningCallbacks{
		OnPlanCreated: func(p planning.Plan) { createdPlan = p },
	}

	result, err := planning.RunPlanningAgent(context.Background(), client, "missing ids", reg, cbs, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Steps should have auto-generated IDs.
	if len(createdPlan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(createdPlan.Steps))
	}
	if createdPlan.Steps[0].ID != "step-1" {
		t.Errorf("step[0].ID = %q, want 'step-1'", createdPlan.Steps[0].ID)
	}
	if createdPlan.Steps[1].ID != "step-2" {
		t.Errorf("step[1].ID = %q, want 'step-2'", createdPlan.Steps[1].ID)
	}
	// ToolArgs should be empty maps, not nil.
	for i, s := range createdPlan.Steps {
		if s.ToolArgs == nil {
			t.Errorf("step[%d].ToolArgs should not be nil", i)
		}
	}
	if result != "all done" {
		t.Errorf("result = %q, want %q", result, "all done")
	}
}

// TestRunPlanningAgent_ReplanWithMissingIDAndNilArgs covers the replan path
// where revised steps have empty IDs and nil tool args.
func TestRunPlanningAgent_ReplanWithMissingIDAndNilArgs(t *testing.T) {
	planResponse := `{
  "goal": "replan ids",
  "steps": [
    {"id":"step-1","description":"first","tool_name":"echo","tool_args":{"message":"x"}}
  ]
}`
	// Replan: revised step has empty ID and null tool_args.
	decisionReplan := `{
  "action": "replan",
  "reason": "need revision",
  "revised_steps_if_replan": [
    {"description":"revised step","tool_name":"echo"}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"replan with defaults done","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionReplan, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	var revisedPlan planning.Plan
	cbs := planning.PlanningCallbacks{
		OnReplan: func(reason string, newPlan planning.Plan) {
			revisedPlan = newPlan
		},
	}

	result, err := planning.RunPlanningAgent(context.Background(), client, "replan ids", reg, cbs, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The revised step should have an auto-generated ID based on its position.
	if len(revisedPlan.Steps) < 2 {
		t.Fatalf("expected at least 2 steps after replan, got %d", len(revisedPlan.Steps))
	}
	revisedStep := revisedPlan.Steps[len(revisedPlan.Steps)-1]
	if revisedStep.ID == "" {
		t.Error("revised step ID should not be empty")
	}
	if revisedStep.ToolArgs == nil {
		t.Error("revised step ToolArgs should not be nil")
	}
	if result != "replan with defaults done" {
		t.Errorf("result = %q, want %q", result, "replan with defaults done")
	}
}

// TestRunPlanningAgent_FinalSummaryCallFails covers the case where the final
// summary LLM call (after all steps exhausted) returns an error.
func TestRunPlanningAgent_FinalSummaryCallFails(t *testing.T) {
	planResponse := `{
  "goal": "summary will fail",
  "steps": [
    {"id":"step-1","description":"echo","tool_name":"echo","tool_args":{"message":"ok"}}
  ]
}`
	// Decision says continue — steps exhaust, then summary call fails.
	decisionContinue := `{"action":"continue","reason":"next"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			nil, // final summary call fails
		},
		errors: []error{nil, nil, fmt.Errorf("summary LLM failure")},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	_, err := planning.RunPlanningAgent(context.Background(), client, "summary will fail", reg, planning.PlanningCallbacks{}, 5)
	if err == nil {
		t.Fatal("expected error when final summary call fails, got nil")
	}
	if !strings.Contains(err.Error(), "final summary call") {
		t.Errorf("error should mention 'final summary call', got: %v", err)
	}
}

// TestRunPlanningAgent_NoToolsAvailable covers running with a nil registry
// (no tools). The plan should still be created; steps referencing tools will
// fail since the registry is empty.
func TestRunPlanningAgent_NoToolsAvailable(t *testing.T) {
	planResponse := `{
  "goal": "no tools",
  "steps": [
    {"id":"step-1","description":"call echo","tool_name":"echo","tool_args":{"message":"hi"}}
  ]
}`
	// The tool will not be found, step fails, decision says done.
	decisionDone := `{"action":"done","result_if_done":"no tools handled","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}

	// nil registry
	result, err := planning.RunPlanningAgent(context.Background(), client, "no tools", nil, planning.PlanningCallbacks{}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "no tools handled" {
		t.Errorf("result = %q, want %q", result, "no tools handled")
	}
}

// TestRunPlanningAgent_EmptyRegistry covers running with an empty registry.
func TestRunPlanningAgent_EmptyRegistry(t *testing.T) {
	planResponse := `{
  "goal": "empty reg",
  "steps": [
    {"id":"step-1","description":"call echo","tool_name":"echo","tool_args":{"message":"hi"}}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"empty registry handled","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}

	reg := tools.NewRegistry() // empty
	result, err := planning.RunPlanningAgent(context.Background(), client, "empty reg", reg, planning.PlanningCallbacks{}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "empty registry handled" {
		t.Errorf("result = %q, want %q", result, "empty registry handled")
	}
}

// TestRunPlanningAgent_FinalSummaryWithMixedStepStatuses covers the final
// summary path with steps that have "done", "failed", and default statuses.
func TestRunPlanningAgent_FinalSummaryWithMixedStepStatuses(t *testing.T) {
	planResponse := `{
  "goal": "mixed statuses",
  "steps": [
    {"id":"step-1","description":"echo ok","tool_name":"echo","tool_args":{"message":"ok"}},
    {"id":"step-2","description":"call missing","tool_name":"nonexistent","tool_args":{}}
  ]
}`
	// Decision after step-1: continue.
	decisionContinue := `{"action":"continue","reason":"next"}`
	// Decision after step-2 (which will fail): continue — will exhaust steps.
	decisionContinue2 := `{"action":"continue","reason":"continue anyway"}`
	summaryResponse := "Mixed status summary."

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue2, Provider: "mock", Model: "mock-model"},
			{Content: summaryResponse, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	result, err := planning.RunPlanningAgent(context.Background(), client, "mixed statuses", reg, planning.PlanningCallbacks{}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != summaryResponse {
		t.Errorf("result = %q, want %q", result, summaryResponse)
	}
}

// TestRunPlanningAgent_TruncateLongResult covers the truncate helper being
// exercised by a step result that exceeds 200 bytes in the decision context.
func TestRunPlanningAgent_TruncateLongResult(t *testing.T) {
	planResponse := `{
  "goal": "long result",
  "steps": [
    {"id":"step-1","description":"echo long","tool_name":"echo","tool_args":{"message":"` + strings.Repeat("A", 300) + `"}}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"long result handled","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	result, err := planning.RunPlanningAgent(context.Background(), client, "long result", reg, planning.PlanningCallbacks{}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "long result handled" {
		t.Errorf("result = %q, want %q", result, "long result handled")
	}
}

// TestRunPlanningAgent_NilCallbacks covers passing zero-value (all nil)
// callbacks — no panics should occur.
func TestRunPlanningAgent_NilCallbacks(t *testing.T) {
	planResponse := `{
  "goal": "nil callbacks",
  "steps": [
    {"id":"step-1","description":"echo","tool_name":"echo","tool_args":{"message":"x"}}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"nil callbacks ok","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	// All callbacks are nil — should not panic.
	result, err := planning.RunPlanningAgent(context.Background(), client, "nil callbacks", reg, planning.PlanningCallbacks{}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "nil callbacks ok" {
		t.Errorf("result = %q, want %q", result, "nil callbacks ok")
	}
}

// TestRunPlanningAgent_PlanGoalFromLLM covers the case where the LLM returns
// a non-empty goal string, which should override the user-supplied one in the
// Plan struct.
func TestRunPlanningAgent_PlanGoalFromLLM(t *testing.T) {
	planResponse := `{
  "goal": "LLM reformulated goal",
  "steps": [
    {"id":"step-1","description":"echo","tool_name":"echo","tool_args":{"message":"x"}}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"goal test done","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	var createdPlan planning.Plan
	cbs := planning.PlanningCallbacks{
		OnPlanCreated: func(p planning.Plan) { createdPlan = p },
	}

	_, err := planning.RunPlanningAgent(context.Background(), client, "user goal", reg, cbs, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The plan goal should come from the LLM since it was non-empty.
	if createdPlan.Goal != "LLM reformulated goal" {
		t.Errorf("plan.Goal = %q, want 'LLM reformulated goal'", createdPlan.Goal)
	}
}

// TestRunPlanningAgent_EmptyGoalFromLLM covers the case where the LLM
// returns an empty goal, so the user-supplied goal is preserved.
func TestRunPlanningAgent_EmptyGoalFromLLM(t *testing.T) {
	planResponse := `{
  "goal": "",
  "steps": [
    {"id":"step-1","description":"echo","tool_name":"echo","tool_args":{"message":"x"}}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"empty goal test done","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	var createdPlan planning.Plan
	cbs := planning.PlanningCallbacks{
		OnPlanCreated: func(p planning.Plan) { createdPlan = p },
	}

	_, err := planning.RunPlanningAgent(context.Background(), client, "user supplied goal", reg, cbs, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty LLM goal — should keep the user-supplied one.
	if createdPlan.Goal != "user supplied goal" {
		t.Errorf("plan.Goal = %q, want 'user supplied goal'", createdPlan.Goal)
	}
}

// TestRunPlanningAgent_FinalSummaryWithFailedStep covers the final summary
// path (steps exhausted) where a step has "failed" status, exercising the
// "failed" case in the final context builder.
func TestRunPlanningAgent_FinalSummaryWithFailedStep(t *testing.T) {
	planResponse := `{
  "goal": "fail then summarize",
  "steps": [
    {"id":"step-1","description":"will fail","tool_name":"fail","tool_args":{}}
  ]
}`
	decisionContinue := `{"action":"continue","reason":"continue past failure"}`
	summaryResponse := "Failed step was summarized."

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: summaryResponse, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(failingTool{name: "fail"})

	result, err := planning.RunPlanningAgent(context.Background(), client, "fail then summarize", reg, planning.PlanningCallbacks{}, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != summaryResponse {
		t.Errorf("result = %q, want %q", result, summaryResponse)
	}
}

// TestRunPlanningAgent_FailedStepInDecisionSummary covers the decision context
// showing "skipped" steps when replanning replaces pending steps.
func TestRunPlanningAgent_FailedStepShowsInDecisionContext(t *testing.T) {
	planResponse := `{
  "goal": "fail step context",
  "steps": [
    {"id":"step-1","description":"will fail","tool_name":"fail","tool_args":{}},
    {"id":"step-2","description":"echo after","tool_name":"echo","tool_args":{"message":"hi"}}
  ]
}`
	// After step-1 fails, decision says continue.
	decisionContinue := `{"action":"continue","reason":"continue despite failure"}`
	// After step-2 succeeds, decision says done.
	decisionDone := `{"action":"done","result_if_done":"completed despite failure","reason":"all done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"}, failingTool{name: "fail"})

	result, err := planning.RunPlanningAgent(context.Background(), client, "fail step context", reg, planning.PlanningCallbacks{}, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "completed despite failure" {
		t.Errorf("result = %q, want %q", result, "completed despite failure")
	}

	// Verify the decision call after step-1 mentioned the failure in context.
	if len(client.calls) < 2 {
		t.Fatal("expected at least 2 LLM calls")
	}
	decisionMsg := client.calls[1].Messages[0].Content
	if !strings.Contains(decisionMsg, "FAILED") {
		t.Errorf("decision context should contain 'FAILED', got: %s", decisionMsg)
	}
}

// TestRunPlanningAgent_MaxStepsWithNegativeValue covers negative maxSteps
// defaulting to DefaultMaxSteps.
func TestRunPlanningAgent_MaxStepsWithNegativeValue(t *testing.T) {
	planResponse := `{
  "goal": "negative max",
  "steps": [
    {"id":"step-1","description":"echo","tool_name":"echo","tool_args":{"message":"a"}}
  ]
}`
	decisionDone := `{"action":"done","result_if_done":"negative max ok","reason":"done"}`

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionDone, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	result, err := planning.RunPlanningAgent(context.Background(), client, "negative max", reg, planning.PlanningCallbacks{}, -5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "negative max ok" {
		t.Errorf("result = %q, want %q", result, "negative max ok")
	}
}

// TestRunPlanningAgent_ExtractJSON_NoJSONBraces covers the extractJSON path
// where the response has no curly braces at all — the raw string is returned
// and JSON parsing fails.
func TestRunPlanningAgent_ExtractJSON_NoJSONBraces(t *testing.T) {
	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: "no json here at all", Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	_, err := planning.RunPlanningAgent(context.Background(), client, "no braces", reg, planning.PlanningCallbacks{}, 5)
	if err == nil {
		t.Fatal("expected error for response with no JSON braces, got nil")
	}
	if !strings.Contains(err.Error(), "parse plan JSON") {
		t.Errorf("error should mention 'parse plan JSON', got: %v", err)
	}
}

// TestRunPlanningAgent_FinalSummaryPath_OnPlanCompleteCallback covers the
// OnPlanComplete callback being invoked on the final-summary path (when
// steps exhaust naturally without a "done" decision).
func TestRunPlanningAgent_FinalSummaryPath_OnPlanCompleteCallback(t *testing.T) {
	planResponse := `{
  "goal": "final callback",
  "steps": [
    {"id":"step-1","description":"echo","tool_name":"echo","tool_args":{"message":"a"}},
    {"id":"step-2","description":"echo2","tool_name":"echo","tool_args":{"message":"b"}}
  ]
}`
	decisionContinue := `{"action":"continue","reason":"next"}`
	summaryResponse := "Final summary via exhaustion."

	client := &mockSequentialClient{
		responses: []*llm.Response{
			{Content: planResponse, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: decisionContinue, Provider: "mock", Model: "mock-model"},
			{Content: summaryResponse, Provider: "mock", Model: "mock-model"},
		},
	}
	reg := tools.NewRegistry(echoTool{name: "echo"})

	var completedResult string
	var completedCalled bool
	cbs := planning.PlanningCallbacks{
		OnPlanComplete: func(result string) {
			completedCalled = true
			completedResult = result
		},
	}

	result, err := planning.RunPlanningAgent(context.Background(), client, "final callback", reg, cbs, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !completedCalled {
		t.Error("OnPlanComplete should be called on the final-summary path")
	}
	if completedResult != summaryResponse {
		t.Errorf("OnPlanComplete result = %q, want %q", completedResult, summaryResponse)
	}
	if result != summaryResponse {
		t.Errorf("returned result = %q, want %q", result, summaryResponse)
	}
}
