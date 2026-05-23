// diagnostician_tools.go — ToolUsingDiagnostician.
//
// Overview:
//   Wraps a base LLMDiagnostician with a tool-use loop: the wrapped
//   diagnostician's request is augmented with a tools.Registry of clinical
//   helpers (drug-interaction check, risk calculator, FHIR-stub lookup)
//   advertised on Request.Tools. The agent then drives a bounded round-trip
//   (default 3, max 6) where the LLM may invoke any advertised tool, the
//   agent executes it, and the result is fed back as the next user turn.
//   Once the model stops calling tools the final response is decoded as a
//   DiagnosisProposal exactly as the base diagnostician would.
//
//   Tool-call history is encoded as marker strings ([tool_call:NAME]ARGS
//   followed by [tool_result:NAME]JSON) so no pkg/llm.Message extension is
//   required — every provider adapter sees plain text content.
//
// Key declarations:
//   - ToolUsingDiagnostician: the wrapper agent
//   - runWithTools: build → loop → decode core
//   - invokeTool: registry lookup + error-to-stub mapping
//   - renderToolCallsForHistory / renderToolResultForHistory: marker formatters
//
// See doc.go for package-level documentation.

package agents
