// Package agentloop implements the "03-Agent-Loop" lesson: the first true
// agent — a tool-calling loop that runs until the model gives a final text
// response.
//
// What this adds on top of lesson 02 (toolcalling):
//
//   - Tool execution  ✓  — ExecuteTool is called for each ToolCall the model makes.
//   - Loop            ✓  — After executing tools, the results are fed back to the
//     model as ToolResult messages and the conversation continues.
//   - Max iterations  ✓  — The loop stops after MaxIterations (default 10) to
//     prevent infinite loops.
//
// TypeScript equivalent: the agent loop in "03-Agent-Loop" lesson.
package agentloop
