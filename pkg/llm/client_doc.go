// client.go — the provider-agnostic LLM client interface and its data types.
//
// Overview:
//   This file defines what every LLM adapter in the package must implement:
//   a Provider/Model identity and a single Complete(ctx, Request) call.
//   It also declares the cross-provider request, response, and structured-output
//   types so agents can swap providers without touching their own code.
//
// Key declarations:
//   - Client: provider-agnostic LLM interface
//   - Request: system prompt + messages + max tokens + temperature + JSONSchema + CacheSystem hint
//   - Response: content + token usage (with CachedTokens) + model/provider/stop_reason
//   - Message: one user/assistant turn
//   - Role: the "user" / "assistant" string type
//   - JSONSchema: name/description/schema for structured outputs
//   - ErrNoMessages, ErrInvalidJSON: failure modes the adapters share
//
// See doc.go for package-level documentation.

package llm
