// Package planning implements Lesson 11 — Long-Horizon Planning.
//
// It teaches agents to decompose a complex multi-step task into a plan,
// execute each step using tools from a tools.Registry, track progress
// across iterations, and recover from failures via replanning.
//
// Core loop: Plan → Execute step → Observe result → Update plan → Repeat.
//
// Usage:
//
//	reg := tools.NewRegistry(tools.ListDirectoryTool{}, tools.ReadFileTool{})
//	result, err := planning.RunPlanningAgent(ctx, client, goal, reg, planning.PlanningCallbacks{
//	    OnPlanCreated:  func(p planning.Plan) { fmt.Println("plan:", p.Goal) },
//	    OnStepStart:    func(s planning.Step, i int) { fmt.Printf("step %d: %s\n", i, s.Description) },
//	    OnStepComplete: func(s planning.Step, i int, result string) { fmt.Println("done:", result) },
//	    OnPlanComplete: func(result string) { fmt.Println("final:", result) },
//	    OnReplan:       func(reason string, newPlan planning.Plan) { fmt.Println("replan:", reason) },
//	})
package planning

// Step is one unit of work inside a Plan. Each step maps to a single tool
// call and tracks its lifecycle from pending through to done or failed.
type Step struct {
	// ID is a short stable identifier for this step (e.g. "step-1").
	ID string `json:"id"`

	// Description is the human-readable explanation of what this step does.
	Description string `json:"description"`

	// ToolName is the name of the tool (from tools.Registry) to invoke.
	ToolName string `json:"tool_name"`

	// ToolArgs are the arguments to pass to the tool. The agent populates
	// these from the LLM's plan output and they are serialised to JSON
	// before being handed to tools.Tool.Invoke.
	ToolArgs map[string]any `json:"tool_args"`

	// Status tracks the step lifecycle. Valid values:
	//   "pending"  — not yet started
	//   "running"  — currently executing
	//   "done"     — completed successfully
	//   "failed"   — execution failed (Error is set)
	//   "skipped"  — bypassed because a replan made it irrelevant
	Status string `json:"status"`

	// Result is the tool output (JSON string) on successful completion.
	Result string `json:"result,omitempty"`

	// Error holds the failure message when Status=="failed".
	Error string `json:"error,omitempty"`
}

// Plan is the full decomposed task produced by the planning agent. It is
// mutated in place as steps are executed and replanning occurs.
type Plan struct {
	// Goal is the original user goal in natural language.
	Goal string `json:"goal"`

	// Steps is the ordered sequence of actions the agent must take.
	Steps []Step `json:"steps"`

	// Current is the index of the step currently being executed.
	Current int `json:"current"`

	// Done is set to true when the plan has reached a terminal state.
	Done bool `json:"done"`

	// Result is the final answer or summary produced when Done=true.
	Result string `json:"result,omitempty"`
}

// PlanningCallbacks is a bag of optional hooks that are called at key points
// during plan execution. All fields are optional — nil values are skipped.
//
// These hooks are designed for logging, UI updates, tracing, and testing.
// They must not block for extended periods.
type PlanningCallbacks struct {
	// OnPlanCreated is called once when the initial plan is produced by the LLM.
	OnPlanCreated func(plan Plan)

	// OnStepStart is called immediately before a step is executed.
	// index is the zero-based position of the step in the current plan.
	OnStepStart func(step Step, index int)

	// OnStepComplete is called after a step finishes (success or failure).
	// result is the tool output string (empty on failure).
	OnStepComplete func(step Step, index int, result string)

	// OnPlanComplete is called once when the plan reaches a Done=true state.
	// result is the final answer returned by the agent.
	OnPlanComplete func(result string)

	// OnReplan is called when the LLM decides the current plan needs revision
	// based on a step result. reason is the LLM's explanation; newPlan is the
	// revised plan that will be executed going forward.
	OnReplan func(reason string, newPlan Plan)
}
