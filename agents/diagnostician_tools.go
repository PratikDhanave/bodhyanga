package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/llm"
	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/tools"
)

// ToolUsingDiagnostician wraps a base LLMDiagnostician and gives the model
// access to a Registry of deterministic clinical tools (drug-interaction,
// risk calculator, FHIR-stub) it can invoke during reasoning. The loop runs
// at most MaxToolRounds tool-call exchanges; once the model stops requesting
// tools the response is decoded as a DiagnosisProposal exactly as the base
// diagnostician would.
//
// The agent's ID remains "diagnostician" so supervisor routing is unchanged.
// Image / RAG behaviour is whatever the Base provides — this wrapper only
// adds the Tools field on the request and the round-trip loop.
//
// Multi-round encoding
//
// pkg/llm's Message type does not carry a separate "tool result" slot, so
// each completed tool call is encoded as an extra assistant turn (the call,
// rendered as a marker string) followed by a user turn (the JSON result).
// This is the least-invasive option that works across the Anthropic, OpenAI,
// and Ollama adapters as they stand; if a future provider needs first-class
// tool_use/tool_result blocks we can extend llm.Message at that point.
type ToolUsingDiagnostician struct {
	// Base supplies the prompt/schema/RAG/fallback wiring. Required.
	Base *LLMDiagnostician

	// Tools is the registry of clinical tools advertised to the LLM. If nil
	// the agent behaves exactly like the base diagnostician (no tools sent,
	// no round-trip loop).
	Tools *tools.Registry

	// MaxToolRounds caps the number of tool-call exchanges before the agent
	// stops looping and falls back to the rule-based diagnostician. Defaults
	// to 3, clamped to [1, 6].
	MaxToolRounds int
}

func (a *ToolUsingDiagnostician) ID() string   { return "diagnostician" }
func (a *ToolUsingDiagnostician) Name() string { return "Diagnostician (LLM + Tools)" }
func (a *ToolUsingDiagnostician) Capabilities() []string {
	return []string{"provide_diagnosis", "differential_diagnosis", "llm_backed", "tool_use"}
}

// HandleMessage implements agent.Agent.
func (a *ToolUsingDiagnostician) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Base == nil {
		return nil, errors.New("tools diagnostician: nil Base")
	}

	state, err := medical.DecodeState(msg.Content)
	if err != nil {
		return nil, err
	}

	proposal, llmErr := a.runWithTools(ctx, state, env)
	if llmErr != nil {
		env.Logf("[diagnostician/tools] %s — falling back to rule-based", llmErr)
		return a.Base.runFallback(ctx, msg, env)
	}

	state.WorkingDiagnosis = proposal.Diagnosis
	env.Logf("[diagnostician/tools] provider=%s model=%s diagnosis=%q confidence=%.0f%%",
		a.Base.Client.Provider(), a.Base.Client.Model(), proposal.Diagnosis, proposal.Confidence*100)
	env.Logf("[diagnostician/tools] rationale: %s", proposal.Rationale)

	content, _ := json.Marshal(proposal)
	stateContent := medical.EncodeState(state)
	verify := agent.NewMessage(
		a.ID(),
		"reasoning_verifier",
		agent.RoleAgent,
		"diagnosis_proposal",
		string(content),
		mergeStateMeta(msg.Metadata, stateContent),
	)
	return []agent.Message{verify}, nil
}

// runWithTools executes the build → loop → decode sequence. It returns the
// final proposal or an error suitable for triggering the rule-based fallback.
func (a *ToolUsingDiagnostician) runWithTools(ctx context.Context, state medical.CaseState, env agent.Environment) (medical.DiagnosisProposal, error) {
	if a.Base.Client == nil {
		return medical.DiagnosisProposal{}, errors.New("llm client not configured")
	}
	timeout := a.Base.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	minConfidence := a.Base.MinConfidence
	if minConfidence == 0 {
		minConfidence = 0.3
	}

	maxRounds := a.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}
	if maxRounds > 6 {
		maxRounds = 6
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, retrieved, _ := a.Base.buildRequest(cctx, state, env)
	if a.Tools != nil {
		req.Tools = a.Tools.LLMTools()
		if len(req.Tools) > 0 {
			env.Logf("[diagnostician/tools] advertising %d tools", len(req.Tools))
		}
	}

	var lastResp *llm.Response
	for round := 0; round < maxRounds; round++ {
		resp, err := a.Base.Client.Complete(cctx, req)
		if err != nil {
			return medical.DiagnosisProposal{}, fmt.Errorf("llm call (round %d): %w", round+1, err)
		}
		lastResp = resp
		if len(resp.ToolCalls) == 0 {
			// Model produced its final answer — drop out and decode below.
			break
		}

		// Record the assistant's tool-call turn and append per-call user turns
		// with the JSON results so the next round can see what was returned.
		req.Messages = append(req.Messages, llm.Message{
			Role:    llm.RoleAssistant,
			Content: renderToolCallsForHistory(resp.ToolCalls),
		})
		for _, call := range resp.ToolCalls {
			result := a.invokeTool(cctx, call, env)
			req.Messages = append(req.Messages, llm.Message{
				Role:    llm.RoleUser,
				Content: renderToolResultForHistory(call.Name, result),
			})
		}

		if round == maxRounds-1 {
			// We hit the cap with the model still requesting tools — bail to
			// the rule-based fallback so the pipeline always produces a
			// proposal.
			return medical.DiagnosisProposal{}, fmt.Errorf("tool loop exceeded MaxToolRounds=%d without a final proposal", maxRounds)
		}
	}

	if lastResp == nil {
		return medical.DiagnosisProposal{}, errors.New("no LLM response produced")
	}

	// Decode the final response as a DiagnosisProposal, mirroring
	// Base.completeAndDecode (including citation reconciliation when RAG
	// retrieved references but the model omitted them).
	var p medical.DiagnosisProposal
	if err := json.Unmarshal([]byte(lastResp.Content), &p); err != nil {
		return medical.DiagnosisProposal{}, fmt.Errorf("decode JSON: %w", err)
	}
	if p.Diagnosis == "" {
		return medical.DiagnosisProposal{}, errors.New("empty diagnosis in LLM output")
	}
	if p.Confidence < minConfidence {
		return medical.DiagnosisProposal{}, fmt.Errorf("confidence %.2f below threshold %.2f", p.Confidence, minConfidence)
	}
	if len(p.Citations) == 0 && len(retrieved) > 0 {
		p.Citations = retrieved
	}

	env.Logf("[diagnostician/tools] tokens in=%d out=%d cached=%d stop=%s",
		lastResp.InputTokens, lastResp.OutputTokens, lastResp.CachedTokens, lastResp.StopReason)
	return p, nil
}

// invokeTool runs one ToolCall against the registry. Unknown tools and tool
// errors are turned into a stub JSON result so the conversation can
// continue rather than aborting the whole diagnosis.
func (a *ToolUsingDiagnostician) invokeTool(ctx context.Context, call llm.ToolCall, env agent.Environment) json.RawMessage {
	if a.Tools == nil {
		env.Logf("[diagnostician/tools] tool call %q ignored: no registry", call.Name)
		return json.RawMessage(`{"error":"no tool registry"}`)
	}
	t, ok := a.Tools.Get(call.Name)
	if !ok {
		env.Logf("[diagnostician/tools] unknown tool %q — returning stub", call.Name)
		return json.RawMessage(`{"error":"unknown tool"}`)
	}
	res, err := t.Invoke(ctx, call.Arguments)
	if err != nil {
		env.Logf("[diagnostician/tools] tool %q failed: %v", call.Name, err)
		// Encode the error as a JSON object so the model still parses it.
		errBody, _ := json.Marshal(map[string]string{"error": err.Error(), "tool": call.Name})
		return errBody
	}
	env.Logf("[diagnostician/tools] tool %q ok (%d bytes)", call.Name, len(res))
	return res
}

// renderToolCallsForHistory formats the assistant's tool calls as a single
// text payload. Each call becomes one line of the form
// [tool_call:NAME]ARGS so a downstream model (or test) can recover the
// sequence without depending on a tool-specific Message extension.
func renderToolCallsForHistory(calls []llm.ToolCall) string {
	var b strings.Builder
	for i, c := range calls {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "[tool_call:%s]%s", c.Name, string(c.Arguments))
	}
	return b.String()
}

// renderToolResultForHistory formats a single tool result for the user-turn
// that follows the assistant's tool-call turn.
func renderToolResultForHistory(name string, result json.RawMessage) string {
	return fmt.Sprintf("[tool_result:%s]%s", name, string(result))
}
