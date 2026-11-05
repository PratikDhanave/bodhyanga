package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/llm"
	"github.com/PratikDhanave/bodhyanga/pkg/tasks"
)

// LLMInboxTriage is a drop-in replacement for InboxTriageAgent that classifies
// inbound patient messages with an LLM. It then constructs the same downstream
// messages (refill_request, prior_auth_request, nurse task + inbox_routed) the
// rule-based agent produces. On any failure (timeout, JSON parse, transport
// error, unknown category) it delegates to a rule-based fallback.
//
// The agent's ID is "inbox_triage" — same as the rule-based one — so the
// supervisor's routing (which targets that ID) does not have to change.
type LLMInboxTriage struct {
	// Client is the LLM provider. Required.
	Client llm.Client

	// Fallback runs when the LLM call fails or returns junk. Required.
	// Typically &InboxTriageAgent{Tasks: ...}.
	Fallback agent.Agent

	// Tasks is the nurse-task queue used when the model classifies a message
	// as urgent_clinical or general. Required.
	Tasks *tasks.Queue

	// Timeout caps a single LLM call. Defaults to 30s.
	Timeout time.Duration

	// MaxTokens caps the response length. Defaults to 512.
	MaxTokens int
}

func (a *LLMInboxTriage) ID() string   { return "inbox_triage" }
func (a *LLMInboxTriage) Name() string { return "Clinical Inbox Triage (LLM)" }
func (a *LLMInboxTriage) Capabilities() []string {
	return []string{"inbox_triage", "message_routing", "llm_backed"}
}

// inboxTriageOutput mirrors the JSON shape the LLM is asked to produce.
type inboxTriageOutput struct {
	Category      string `json:"category"`
	Medication    string `json:"medication,omitempty"`
	ProcedureCode string `json:"procedure_code,omitempty"`
}

func (a *LLMInboxTriage) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var item inboxMessage
	if err := json.Unmarshal([]byte(msg.Content), &item); err != nil {
		env.Logf("[inbox_triage/llm] invalid message: %v", err)
		return nil, err
	}

	out, llmErr := a.callLLM(ctx, item, env)
	if llmErr != nil {
		env.Logf("[inbox_triage/llm] %s — falling back to rule-based", llmErr)
		return a.runFallback(ctx, msg, env)
	}

	env.Logf("[inbox_triage/llm] provider=%s patient=%s category=%s subject=%q",
		a.Client.Provider(), item.PatientID, out.Category, item.Subject)

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = item.PatientID
	meta["inbox_category"] = out.Category

	switch out.Category {
	case "refill":
		med := out.Medication
		if med == "" {
			med = "unknown"
		}
		body, _ := json.Marshal(map[string]string{
			"patient_id": item.PatientID,
			"medication": med,
			"reason":     "patient inbox",
		})
		return []agent.Message{agent.NewMessage(a.ID(), "refill_manager",
			agent.RoleAgent, "refill_request", string(body), meta)}, nil
	case "prior_auth":
		code := out.ProcedureCode
		if code == "" {
			code = "unknown"
		}
		body, _ := json.Marshal(map[string]string{
			"patient_id": item.PatientID,
			"code":       code,
		})
		return []agent.Message{agent.NewMessage(a.ID(), "prior_auth",
			agent.RoleAgent, "prior_auth_request", string(body), meta)}, nil
	case "urgent_clinical":
		if a.Tasks == nil {
			env.Logf("[inbox_triage/llm] no task queue configured — falling back to rule-based")
			return a.runFallback(ctx, msg, env)
		}
		a.Tasks.Create(tasks.NurseTask{
			Kind: "inbox", PatientID: item.PatientID, Priority: "urgent",
			DueAt:   time.Now().Add(2 * time.Hour),
			Subject: "Urgent inbox: " + item.Subject,
			Detail:  item.Body,
		})
	case "general":
		if a.Tasks == nil {
			env.Logf("[inbox_triage/llm] no task queue configured — falling back to rule-based")
			return a.runFallback(ctx, msg, env)
		}
		a.Tasks.Create(tasks.NurseTask{
			Kind: "inbox", PatientID: item.PatientID, Priority: "normal",
			DueAt:   time.Now().Add(24 * time.Hour),
			Subject: item.Subject, Detail: item.Body,
		})
	default:
		env.Logf("[inbox_triage/llm] unknown category %q — falling back to rule-based", out.Category)
		return a.runFallback(ctx, msg, env)
	}

	routed := agent.NewMessage(a.ID(), "", agent.RoleAgent, "inbox_routed",
		`{"category":"`+out.Category+`"}`, meta)
	return []agent.Message{routed}, nil
}

func (a *LLMInboxTriage) callLLM(ctx context.Context, item inboxMessage, env agent.Environment) (inboxTriageOutput, error) {
	if a.Client == nil {
		return inboxTriageOutput{}, errors.New("llm client not configured")
	}
	timeout := a.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	maxTokens := a.MaxTokens
	if maxTokens == 0 {
		maxTokens = 512
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	itemJSON, _ := json.MarshalIndent(item, "", "  ")
	grammar := gbnfForInboxTriage()
	req := llm.Request{
		System:      inboxTriageSystemPrompt,
		CacheSystem: true,
		Messages: []llm.Message{{
			Role:    llm.RoleUser,
			Content: "Inbox message:\n" + string(itemJSON) + "\n\nClassify and extract entities.",
		}},
		MaxTokens:   maxTokens,
		Temperature: 0.0,
		JSONSchema: &llm.JSONSchema{
			Name:        "classify_inbox_message",
			Description: "Classify a patient inbox message into refill | prior_auth | urgent_clinical | general and extract entities.",
			Schema:      json.RawMessage(inboxTriageSchema),
		},
		Grammar: &grammar,
	}

	resp, err := a.Client.Complete(cctx, req)
	if err != nil {
		return inboxTriageOutput{}, fmt.Errorf("llm call: %w", err)
	}

	var out inboxTriageOutput
	if err := json.Unmarshal([]byte(resp.Content), &out); err != nil {
		return inboxTriageOutput{}, fmt.Errorf("decode JSON: %w", err)
	}
	switch out.Category {
	case "refill", "prior_auth", "urgent_clinical", "general":
		// ok
	default:
		return inboxTriageOutput{}, fmt.Errorf("invalid category %q", out.Category)
	}

	env.Logf("[inbox_triage/llm] tokens in=%d out=%d cached=%d stop=%s",
		resp.InputTokens, resp.OutputTokens, resp.CachedTokens, resp.StopReason)
	return out, nil
}

func (a *LLMInboxTriage) runFallback(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Fallback == nil {
		return nil, errors.New("no fallback configured")
	}
	return a.Fallback.HandleMessage(ctx, msg, env)
}

// inboxTriageSystemPrompt is sent (cached) on every LLM inbox triage call.
const inboxTriageSystemPrompt = `You are the Inbox Triage agent for a virtual primary-care office. Your job is to classify inbound patient messages and extract the entities downstream agents need.

Classify the message into exactly one of:
  - refill           : the patient is asking to renew a prescription medication.
  - prior_auth       : the message concerns prior authorization for a procedure or imaging study (MRI, CT, PET, DEXA, etc.).
  - urgent_clinical  : the message describes a potentially serious clinical symptom (chest pain, severe shortness of breath, syncope, bleeding, suspected stroke, etc.).
  - general          : routine administrative or non-urgent clinical messages.

Also extract:
  - medication       : the medication name (lowercased) when category is refill, else empty.
  - procedure_code   : the imaging or procedure code (e.g. MRI, CT, PET, DEXA) when category is prior_auth, else empty.

Constraints:
  - Be conservative — when in doubt between urgent_clinical and general, prefer urgent_clinical.
  - Do not provide clinical advice or treatment plans.
  - Return ONLY JSON matching the supplied tool schema. No prose outside the schema fields.`

// inboxTriageCategories is the closed vocabulary the LLM may emit for the
// "category" field. Kept in lockstep with the enum in inboxTriageSchema and
// the switch statement in HandleMessage. If you add a category here, add it
// to both the schema and the routing switch.
var inboxTriageCategories = []string{"refill", "prior_auth", "urgent_clinical", "general"}

// gbnfForInboxTriage returns a sampler-level enum grammar over the inbox
// triage categories. The agent's wire format is a JSON object — the enum
// constraint here mirrors the JSON-Schema enum and is the intent expressed
// to grammar-aware providers (kronk). Providers without sampler-level GBNF
// (anthropic, openai, ollama) log a one-time warning and ignore the field;
// the JSONSchema layer + agent-level "unknown category → fallback" path
// still enforce the closed vocabulary.
func gbnfForInboxTriage() llm.GrammarSpec {
	return llm.GBNFEnum(inboxTriageCategories)
}

// inboxTriageSchema is the JSON Schema for the inbox-triage output.
const inboxTriageSchema = `{
  "type": "object",
  "properties": {
    "category": {
      "type": "string",
      "enum": ["refill", "prior_auth", "urgent_clinical", "general"],
      "description": "The triage category."
    },
    "medication": {
      "type": "string",
      "description": "Medication name when category is refill, else empty string."
    },
    "procedure_code": {
      "type": "string",
      "description": "Procedure / imaging code when category is prior_auth, else empty string."
    }
  },
  "required": ["category"],
  "additionalProperties": false
}`
