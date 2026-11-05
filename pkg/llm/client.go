package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// Client is the provider-agnostic LLM interface used by Bodhyanga agents.
type Client interface {
	// Provider returns a short identifier for the underlying provider
	// (e.g. "anthropic", "openai", "mock"). Used in logs and bench output.
	Provider() string

	// Model returns the model identifier the client is configured for
	// (e.g. "claude-sonnet-4-6", "gpt-4o").
	Model() string

	// Complete sends the request and returns a single completion. If the
	// request specifies a JSONSchema, the returned Content is the JSON
	// document (already extracted from any tool-use wrapper).
	Complete(ctx context.Context, req Request) (*Response, error)
}

// Request carries everything needed for one completion.
type Request struct {
	// System is the system prompt. Sent once at the start of the conversation.
	System string

	// Messages is the conversation in turn order. Each entry alternates
	// between user and assistant. Minimum one entry (the initial user prompt).
	Messages []Message

	// MaxTokens caps the response length. Required.
	MaxTokens int

	// Temperature controls sampling randomness. 0.0 is most deterministic;
	// 1.0 is most random. Clinical reasoning benefits from low values (0.0-0.3).
	Temperature float64

	// JSONSchema, when set, asks the provider to return JSON matching the
	// schema. The provider-specific adapter wires this to tool use
	// (Anthropic) or response_format (OpenAI).
	JSONSchema *JSONSchema

	// CacheSystem hints that the system prompt is reused across many calls
	// and should be eligible for prompt caching where supported.
	CacheSystem bool

	// Tools, when non-empty, advertises tools the model may call. The
	// model's choice (if any) shows up in Response.ToolCalls. When both
	// Tools and JSONSchema are set, providers that cannot combine them
	// (Anthropic) prefer JSONSchema and silently ignore Tools.
	Tools []Tool

	// Grammar, when non-nil, requests a sampler-level structural guarantee
	// on the response — providers that support it (llama.cpp-family,
	// kronk) will refuse any token that breaks the grammar, so the
	// response is guaranteed well-formed without a retry. Providers that
	// do not support grammars emit a one-time warning and ignore the
	// field; the JSONSchema path and the agent's retry-on-bad-JSON
	// fallback still run. See grammar.go for the spec format and helpers
	// (GBNFJSONObject, GBNFEnum, GBNFRegex).
	Grammar *GrammarSpec
}

// Message is one turn in the conversation.
type Message struct {
	Role    Role
	Content string

	// Images, when non-empty, attaches one or more images to this message.
	// Only meaningful for user messages with vision-capable models.
	Images []Image

	// ToolCalls is set on assistant-role messages when the model invoked
	// one or more tools. Callers running a multi-turn tool loop must carry
	// these forward in the next request so the provider sees the full
	// conversation history (Anthropic requires this for tool_result
	// correlation). Populated from Response.ToolCalls.
	ToolCalls []ToolCall

	// ToolResults is set on user-role messages that return tool outputs
	// to the model. Each entry must correlate (via ToolUseID) to a
	// ToolCall.ID in the preceding assistant message.
	ToolResults []ToolResult
}

// ToolResult carries the output of a single tool call. ToolUseID must
// match the ToolCall.ID that triggered the invocation; providers use this
// to correlate the result with the tool_use content block.
type ToolResult struct {
	ToolUseID string // matches ToolCall.ID (e.g. "toolu_xxxx" for Anthropic)
	Content   string // return value: JSON string or plain text
}

// Image is an inline image attached to a Message. Data is the raw bytes
// (not base64) and MediaType is the MIME type, e.g. "image/png" or
// "image/jpeg". Adapters base64-encode Data themselves.
type Image struct {
	MediaType string
	Data      []byte
}

// Tool advertises a function the model may call. Schema is the JSON
// Schema document describing its input arguments.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// ToolCall is one function invocation produced by the model. Arguments
// is the raw JSON the provider returned (OpenAI sends a string, Anthropic
// and Ollama send an object — adapters normalize both to JSON bytes).
// ID is the provider-generated correlation identifier (e.g. "toolu_xxxx"
// for Anthropic) used to match tool_result content blocks in the next turn.
type ToolCall struct {
	ID        string          // provider-generated correlation ID; empty for providers that omit it
	Name      string
	Arguments json.RawMessage
}

// Role is "user" or "assistant".
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// JSONSchema describes a structured output the model must produce.
type JSONSchema struct {
	// Name labels the schema. For Anthropic this becomes the tool name.
	Name string

	// Description briefly tells the model what the schema is for. Shown
	// to the model as part of the tool description / schema metadata.
	Description string

	// Schema is the raw JSON Schema document (a subset is supported across
	// providers — keep it to type/properties/required/enum/items).
	Schema json.RawMessage
}

// Response is what the model returned.
type Response struct {
	// Content is the assistant's text reply, or the JSON document if a
	// JSONSchema was requested.
	Content string

	// InputTokens is the number of tokens billed for the request.
	InputTokens int

	// OutputTokens is the number of tokens billed for the response.
	OutputTokens int

	// CachedTokens is how many input tokens hit the prompt cache (where
	// reported by the provider).
	CachedTokens int

	// Model echoes back the model that produced the response.
	Model string

	// Provider is the short provider identifier ("anthropic", "openai").
	Provider string

	// StopReason is the provider's stop reason ("end_turn", "max_tokens",
	// "tool_use", "stop", etc.).
	StopReason string

	// ToolCalls is populated when the model invoked one or more advertised
	// Tools. Empty when no tool was called or when JSONSchema-style
	// structured output was used instead.
	ToolCalls []ToolCall
}

// ErrNoMessages is returned when Complete is called with an empty Messages
// slice — the providers all reject that, so we fail fast.
var ErrNoMessages = errors.New("llm: request has no messages")

// ErrInvalidJSON is returned when JSONSchema was requested but the provider
// returned content that didn't parse.
var ErrInvalidJSON = errors.New("llm: response was not valid JSON for the requested schema")
