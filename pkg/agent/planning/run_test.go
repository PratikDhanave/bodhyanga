package planning_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent/planning"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/tools"
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

func (e echoTool) Name() string             { return e.name }
func (e echoTool) Description() string      { return "Echo the message argument back." }
func (e echoTool) Schema() json.RawMessage  { return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`) }
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
