package intro

// SystemPrompt is the default system prompt for the intro agent.
//
// TypeScript equivalent: SYSTEM_PROMPT in src/agent/system/prompt.ts
//
// This prompt positions the agent as a general-purpose assistant. Later
// lessons specialise the system prompt for specific domains (coding agent,
// research agent, medical triage agent). Changing the system prompt is one
// of the highest-leverage ways to alter agent behaviour — try the exercises
// in the lesson to see how different prompts change responses.
const SystemPrompt = `You are a helpful AI assistant.

Your job is to answer questions clearly and concisely. When you don't know
something, say so rather than guessing.

Guidelines:
- Keep responses focused and to the point
- Use examples when they help clarify a concept
- If a task requires actions you cannot take (browsing the web, writing files,
  calling APIs), explain what you would do and why you cannot do it yet`
