package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// Client is the provider-agnostic LLM interface used by Bodh agents.
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
}

// Message is one turn in the conversation.
type Message struct {
	Role    Role
	Content string

	// Images, when non-empty, attaches one or more images to this message.
	// Only meaningful for user messages with vision-capable models.
	Images []Image
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
type ToolCall struct {
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

// StreamChunk is one delta in a streaming response. Either Content has a
// partial text chunk, or ToolCall is populated when the model emits a tool
// invocation mid-stream, or Done=true to signal end of stream with optional
// final Usage stats.
type StreamChunk struct {
	Content  string    // partial text since last chunk; empty on tool/done chunks
	ToolCall *ToolCall // when the model invoked a tool mid-stream
	Done     bool      // true on the LAST chunk; carries the final Response too
	Final    *Response // populated when Done=true; nil otherwise
	Err      error     // populated if the stream terminated abnormally
}

// Streamer is an optional interface a Client implements when it supports
// token streaming. Adapters that don't implement it can still be used via
// the regular Complete method; callers fall back gracefully.
type Streamer interface {
	Stream(ctx context.Context, req Request) (<-chan StreamChunk, error)
}

// CanStream reports whether a Client also implements Streamer.
func CanStream(c Client) bool {
	_, ok := c.(Streamer)
	return ok
}
