// diagnostician_tools_test.go — ToolUsingDiagnostician unit tests.
//
// Overview:
//   Drives the tool-using diagnostician with the scriptedLLM (from
//   diagnostician_reflexion_test.go) so each test can sequence a multi-turn
//   tool conversation. Coverage:
//     - NoToolCallsBreaksImmediately:    1 LLM call, no tools invoked
//     - OneToolCallThenFinal:            tool executed, history markers asserted
//     - BoundedByMaxToolRounds:          loop cap → rule-based fallback
//     - UnknownToolNameLogsAndContinues: stub error result, loop continues
//
//   A small spyTool wraps a real tools.Tool so we can assert Invoke was
//   actually called, separate from the LLM call sequence.
//
// Key declarations:
//   - newToolsTestState / defaultRegistry / spyTool: per-test helpers
//   - finalProposalResponse / toolCallResponse:    canned scriptedLLM steps
//
// See doc.go for package-level documentation.

package agents
