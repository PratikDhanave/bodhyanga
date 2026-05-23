// types.go — agent and environment interfaces plus protocol re-exports.
//
// Overview:
//   Declares the small Agent contract (ID, Name, Capabilities, HandleMessage)
//   that every concrete agent implements, and the Environment abstraction
//   that provides clock and logging to handlers. Also re-exports the core
//   protocol message types so agents don't have to import pkg/protocol just
//   to manipulate messages.
//
// Key declarations:
//   - Agent: minimum interface for any agent (orchestrator dispatches to this).
//   - Environment: shared services (Now, Logf) passed to HandleMessage.
//   - Message / MessageRole: re-exports of protocol.Message / protocol.MessageRole.
//   - RoleUser, RoleSystem, RoleAgent, RoleTool, RoleObserver, RoleEvaluator: role constants.
//   - NewMessage: thin constructor delegating to protocol.NewMessage.
//
// See doc.go for package-level documentation.

package agent
