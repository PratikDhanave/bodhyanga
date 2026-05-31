package agentloop

import "encoding/json"

// AgentCallbacks provides optional lifecycle hooks. Nil fields are ignored.
type AgentCallbacks struct {
	OnStart      func(userMessage string)
	OnToolCall   func(name string, args json.RawMessage)
	OnToolResult func(name string, result string)
	OnFinish     func(response string)
}
