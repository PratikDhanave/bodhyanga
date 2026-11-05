package planning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/tools"
)

const (
	// DefaultModel is the default Ollama model for this lesson.
	DefaultModel = "qwen2.5:7b"

	// DefaultMaxSteps caps the total number of step executions across all
	// planning iterations to prevent runaway loops.
	DefaultMaxSteps = 20
)

// planJSON is the shape the LLM must return for the initial plan.
type planJSON struct {
	Goal  string     `json:"goal"`
	Steps []stepJSON `json:"steps"`
}

// stepJSON is the per-step shape from the LLM.
type stepJSON struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	ToolName    string         `json:"tool_name"`
	ToolArgs    map[string]any `json:"tool_args"`
}

// stepDecisionJSON is what the LLM returns after each step execution.
type stepDecisionJSON struct {
	// Action is one of "continue", "replan", or "done".
	Action string `json:"action"`

	// ResultIfDone is the final answer when Action=="done".
	ResultIfDone string `json:"result_if_done,omitempty"`

	// RevisedStepsIfReplan replaces the remaining steps when Action=="replan".
	RevisedStepsIfReplan []stepJSON `json:"revised_steps_if_replan,omitempty"`

	// Reason is an optional explanation of the decision.
	Reason string `json:"reason,omitempty"`
}

// toolListSummary builds a concise list of available tools for the system prompt.
func toolListSummary(reg *tools.Registry) string {
	if reg == nil {
		return "(no tools available)"
	}
	ts := reg.List()
	if len(ts) == 0 {
		return "(no tools available)"
	}
	lines := make([]string, 0, len(ts))
	for _, t := range ts {
		lines = append(lines, fmt.Sprintf("  - %s: %s", t.Name(), t.Description()))
	}
	return strings.Join(lines, "\n")
}

// planningSystemPrompt returns the system prompt for the initial planning call.
func planningSystemPrompt(reg *tools.Registry) string {
	return fmt.Sprintf(`You are a planning agent. Your job is to decompose a user goal into
a sequence of executable steps, where each step calls exactly one available tool.

Available tools:
%s

Return ONLY a JSON object (no markdown, no prose) in this exact shape:
{
  "goal": "<original goal>",
  "steps": [
    {
      "id": "step-1",
      "description": "<what this step does>",
      "tool_name": "<tool name from the list above>",
      "tool_args": { "<arg>": "<value>" }
    }
  ]
}

Rules:
- Every step must use a tool from the available tools list.
- tool_args must match the tool's expected input schema.
- Keep steps focused and atomic — one tool call per step.
- Do not include steps for tools that are not listed.
- Return raw JSON only.`, toolListSummary(reg))
}

// decisionSystemPrompt returns the system prompt for the post-step decision call.
func decisionSystemPrompt() string {
	return `You are a planning agent evaluating progress on a multi-step task.

Given the result of the most recently executed step, decide what to do next.
Return ONLY a JSON object (no markdown, no prose) in this exact shape:

{
  "action": "<continue|replan|done>",
  "result_if_done": "<final answer if action is done>",
  "revised_steps_if_replan": [
    {
      "id": "step-N",
      "description": "<what this step does>",
      "tool_name": "<tool name>",
      "tool_args": { "<arg>": "<value>" }
    }
  ],
  "reason": "<brief explanation>"
}

Action meanings:
- "continue": the plan is on track; proceed to the next step as planned.
- "replan":   the step result revealed the plan needs adjustment; provide revised_steps_if_replan.
- "done":     the goal is fully achieved; provide result_if_done with the final answer.

Return raw JSON only.`
}

// extractJSON tries to extract a JSON object from a potentially prose-wrapped
// response. It strips markdown code fences and finds the outermost {...}.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip markdown code fences if present.
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		var inner []string
		for _, l := range lines {
			trimmed := strings.TrimSpace(l)
			if strings.HasPrefix(trimmed, "```") {
				continue
			}
			inner = append(inner, l)
		}
		s = strings.Join(inner, "\n")
		s = strings.TrimSpace(s)
	}

	// Find the outermost JSON object boundaries.
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end >= start {
		return s[start : end+1]
	}
	return s
}

// invokeStep executes the tool referenced by step and returns the raw result string.
func invokeStep(ctx context.Context, reg *tools.Registry, step Step) (string, error) {
	t, ok := reg.Get(step.ToolName)
	if !ok {
		return "", fmt.Errorf("tool %q not found in registry", step.ToolName)
	}

	argsJSON, err := json.Marshal(step.ToolArgs)
	if err != nil {
		return "", fmt.Errorf("marshal tool args for %q: %w", step.ToolName, err)
	}

	result, err := t.Invoke(ctx, argsJSON)
	if err != nil {
		return "", fmt.Errorf("invoke %q: %w", step.ToolName, err)
	}
	return string(result), nil
}

// RunPlanningAgent is the entry point for Lesson 11 — Long-Horizon Planning.
//
// Algorithm:
//  1. Ask the LLM to produce a JSON plan: {goal, steps:[{id, description, tool_name, tool_args}]}
//  2. Execute steps in order using the tools.Registry.
//  3. After each step, ask the LLM: "Given the step result, should we continue,
//     replan, or are we done?" and handle each case.
//  4. Loop until done or maxSteps is reached.
//
// callbacks may be nil or have nil fields — nil hooks are silently skipped.
// maxSteps <= 0 defaults to DefaultMaxSteps.
func RunPlanningAgent(ctx context.Context, client llm.Client, goal string, reg *tools.Registry, callbacks PlanningCallbacks, maxSteps int) (string, error) {
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	// -------------------------------------------------------------------------
	// Step 1: Ask the LLM to produce a plan.
	// -------------------------------------------------------------------------
	planResp, err := client.Complete(ctx, llm.Request{
		System: planningSystemPrompt(reg),
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: fmt.Sprintf("Goal: %s", goal)},
		},
		MaxTokens:   2048,
		Temperature: 0.2,
	})
	if err != nil {
		return "", fmt.Errorf("planning: initial plan call: %w", err)
	}

	rawPlan := extractJSON(planResp.Content)
	var pj planJSON
	if err := json.Unmarshal([]byte(rawPlan), &pj); err != nil {
		return "", fmt.Errorf("planning: parse plan JSON: %w\nraw response: %s", err, planResp.Content)
	}

	// Convert to our Plan type.
	plan := Plan{
		Goal: goal,
	}
	if pj.Goal != "" {
		plan.Goal = pj.Goal
	}
	plan.Steps = make([]Step, len(pj.Steps))
	for i, sj := range pj.Steps {
		id := sj.ID
		if id == "" {
			id = fmt.Sprintf("step-%d", i+1)
		}
		args := sj.ToolArgs
		if args == nil {
			args = map[string]any{}
		}
		plan.Steps[i] = Step{
			ID:          id,
			Description: sj.Description,
			ToolName:    sj.ToolName,
			ToolArgs:    args,
			Status:      "pending",
		}
	}

	if callbacks.OnPlanCreated != nil {
		callbacks.OnPlanCreated(plan)
	}

	// Track total steps executed to enforce maxSteps.
	totalExecuted := 0

	// -------------------------------------------------------------------------
	// Main execution loop.
	// -------------------------------------------------------------------------
	for !plan.Done {
		if plan.Current >= len(plan.Steps) {
			// No more steps but not explicitly done — ask the LLM to finalise.
			break
		}

		if totalExecuted >= maxSteps {
			return "", fmt.Errorf("planning: reached max steps (%d) without completing the goal", maxSteps)
		}

		step := &plan.Steps[plan.Current]
		step.Status = "running"

		if callbacks.OnStepStart != nil {
			callbacks.OnStepStart(*step, plan.Current)
		}

		// Execute the tool.
		result, execErr := invokeStep(ctx, reg, *step)
		totalExecuted++

		if execErr != nil {
			step.Status = "failed"
			step.Error = execErr.Error()
		} else {
			step.Status = "done"
			step.Result = result
		}

		if callbacks.OnStepComplete != nil {
			callbacks.OnStepComplete(*step, plan.Current, result)
		}

		// -------------------------------------------------------------------------
		// Step 3: Ask the LLM what to do next given this step's result.
		// -------------------------------------------------------------------------
		var stepSummary string
		if execErr != nil {
			stepSummary = fmt.Sprintf("Step %q (%s) FAILED: %s", step.ID, step.ToolName, step.Error)
		} else {
			stepSummary = fmt.Sprintf("Step %q (%s) SUCCEEDED. Result: %s", step.ID, step.ToolName, result)
		}

		// Summarise steps completed so far.
		var completedSummary strings.Builder
		completedSummary.WriteString(fmt.Sprintf("Goal: %s\n\n", plan.Goal))
		completedSummary.WriteString("Completed steps so far:\n")
		for i := 0; i <= plan.Current; i++ {
			s := plan.Steps[i]
			switch s.Status {
			case "done":
				fmt.Fprintf(&completedSummary, "  [done]   %s: %s (result: %s)\n", s.ID, s.Description, truncate(s.Result, 200))
			case "failed":
				fmt.Fprintf(&completedSummary, "  [failed] %s: %s (error: %s)\n", s.ID, s.Description, s.Error)
			case "skipped":
				fmt.Fprintf(&completedSummary, "  [skip]   %s: %s\n", s.ID, s.Description)
			}
		}

		remaining := plan.Steps[plan.Current+1:]
		if len(remaining) > 0 {
			completedSummary.WriteString("\nRemaining planned steps:\n")
			for _, s := range remaining {
				fmt.Fprintf(&completedSummary, "  [pending] %s: %s\n", s.ID, s.Description)
			}
		}

		decisionResp, err := client.Complete(ctx, llm.Request{
			System: decisionSystemPrompt(),
			Messages: []llm.Message{
				{Role: llm.RoleUser, Content: fmt.Sprintf("%s\n\nMost recent step result:\n%s\n\nShould we continue, replan, or are we done?", completedSummary.String(), stepSummary)},
			},
			MaxTokens:   1024,
			Temperature: 0.2,
		})
		if err != nil {
			return "", fmt.Errorf("planning: decision call after step %q: %w", step.ID, err)
		}

		rawDecision := extractJSON(decisionResp.Content)
		var decision stepDecisionJSON
		if err := json.Unmarshal([]byte(rawDecision), &decision); err != nil {
			// If we can't parse the decision, default to continue.
			decision = stepDecisionJSON{Action: "continue"}
		}

		switch strings.ToLower(strings.TrimSpace(decision.Action)) {
		case "done":
			plan.Done = true
			plan.Result = decision.ResultIfDone
			if callbacks.OnPlanComplete != nil {
				callbacks.OnPlanComplete(plan.Result)
			}
			return plan.Result, nil

		case "replan":
			// Build a new step list from the LLM's revised steps.
			newSteps := make([]Step, len(decision.RevisedStepsIfReplan))
			for i, sj := range decision.RevisedStepsIfReplan {
				id := sj.ID
				if id == "" {
					id = fmt.Sprintf("step-%d", plan.Current+i+2)
				}
				args := sj.ToolArgs
				if args == nil {
					args = map[string]any{}
				}
				newSteps[i] = Step{
					ID:          id,
					Description: sj.Description,
					ToolName:    sj.ToolName,
					ToolArgs:    args,
					Status:      "pending",
				}
			}

			// Keep completed steps + prepend, advance Current, reset remaining.
			done := plan.Steps[:plan.Current+1]
			revised := Plan{
				Goal:    plan.Goal,
				Steps:   append(done, newSteps...),
				Current: plan.Current + 1,
			}

			if callbacks.OnReplan != nil {
				callbacks.OnReplan(decision.Reason, revised)
			}
			plan = revised
			// Don't advance Current here — we set it to plan.Current+1 already.
			continue

		default: // "continue"
			plan.Current++
		}
	}

	// -------------------------------------------------------------------------
	// All steps exhausted — ask the LLM for a final summary.
	// -------------------------------------------------------------------------
	var finalContext strings.Builder
	finalContext.WriteString(fmt.Sprintf("Goal: %s\n\nAll steps have been executed:\n", plan.Goal))
	for _, s := range plan.Steps {
		switch s.Status {
		case "done":
			fmt.Fprintf(&finalContext, "  [done]   %s: %s (result: %s)\n", s.ID, s.Description, truncate(s.Result, 200))
		case "failed":
			fmt.Fprintf(&finalContext, "  [failed] %s: %s (error: %s)\n", s.ID, s.Description, s.Error)
		default:
			fmt.Fprintf(&finalContext, "  [%s]  %s: %s\n", s.Status, s.ID, s.Description)
		}
	}
	finalContext.WriteString("\nBased on all of the above, what is the final answer to the user's goal?")

	summaryResp, err := client.Complete(ctx, llm.Request{
		System:      "You are a helpful assistant. Summarise the results of a completed multi-step task for the user.",
		Messages:    []llm.Message{{Role: llm.RoleUser, Content: finalContext.String()}},
		MaxTokens:   512,
		Temperature: 0.3,
	})
	if err != nil {
		return "", fmt.Errorf("planning: final summary call: %w", err)
	}

	plan.Done = true
	plan.Result = summaryResp.Content

	if callbacks.OnPlanComplete != nil {
		callbacks.OnPlanComplete(plan.Result)
	}
	return plan.Result, nil
}

// truncate returns s truncated to n bytes with a "…" suffix when cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
