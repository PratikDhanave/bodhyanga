package a2a

// AgentCard describes an agent's identity and capabilities (served at /.well-known/agent.json).
type AgentCard struct {
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Version      string       `json:"version"`
	URL          string       `json:"url"`
	Capabilities Capabilities `json:"capabilities"`
	Skills       []Skill      `json:"skills"`
}

// Capabilities lists optional A2A protocol features the agent supports.
type Capabilities struct {
	Streaming  bool `json:"streaming"`
	PushNotify bool `json:"pushNotifications"`
}

// Skill describes a single capability the agent can execute.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

// TaskState represents the lifecycle state of a task.
type TaskState string

const (
	TaskStateSubmitted TaskState = "submitted"
	TaskStateWorking   TaskState = "working"
	TaskStateCompleted TaskState = "completed"
	TaskStateFailed    TaskState = "failed"
)

// Task is the unit of work in the A2A protocol.
type Task struct {
	ID      string         `json:"id"`
	State   TaskState      `json:"state"`
	Input   map[string]any `json:"input"`
	Output  *TaskOutput    `json:"output,omitempty"`
	Error   string         `json:"error,omitempty"`
	Created string         `json:"createdAt"`
	Updated string         `json:"updatedAt"`
}

// TaskOutput holds the result of a completed task.
type TaskOutput struct {
	Text string `json:"text"`
}

// SendTaskRequest is the body of a POST /tasks request.
type SendTaskRequest struct {
	Input map[string]any `json:"input"`
	// SkillID selects which skill to invoke (optional).
	SkillID string `json:"skillId,omitempty"`
}
