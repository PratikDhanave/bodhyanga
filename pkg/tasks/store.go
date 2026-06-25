package tasks

// TaskStore is the nurse-task surface the workflow agents depend on. Agents
// only ever enqueue work, so the agent-facing contract is a single method —
// Create. List is included because the backend HTTP layer (the future
// cmd/api, and today's cmd/care task handler) reads the queue, and the
// Postgres-backed adapter must satisfy both halves from one type.
//
// Why this exists (agent decoupling, Phase 0): agents currently hold the
// concrete in-memory *Queue. Depending on this interface lets a later step
// inject a Postgres-backed adapter (same signatures) so task state survives
// across separately deployed agent containers, without touching agent logic.
//
// Keep this NARROW. UpdateStatus / FilterOpen / CountByKind live on *Queue
// but no agent calls them, so they stay off the interface until a consumer
// actually needs them.
type TaskStore interface {
	Create(t NurseTask) NurseTask
	List() []NurseTask
}

// Compile-time assertion that the in-memory queue satisfies the interface.
var _ TaskStore = (*Queue)(nil)
