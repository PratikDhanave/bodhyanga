package toolcalling

// SystemPrompt is the default system prompt for the tool-calling agent.
//
// TypeScript equivalent: SYSTEM_PROMPT in src/agent/system/prompt.ts
//
// The prompt instructs the model to use tools when they are relevant rather
// than guessing from training data. This is important for factual, real-time
// queries like the current date — without the instruction the model might
// answer from training-data assumptions rather than calling getDateTime.
const SystemPrompt = `You are a helpful AI assistant with access to tools.

When answering questions:
- Use tools when they provide accurate, real-time information
- Call getDateTime whenever the user asks about the current time, date, or anything time-sensitive
- Answer directly from your knowledge when tools are not needed

Keep your responses concise and accurate.`
