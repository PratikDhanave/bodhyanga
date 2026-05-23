// registry.go — agent registry interface and in-memory implementation.
//
// Overview:
//   Answers "which agents exist and what can they do?" — a distinct concern
//   from the bus's "how do messages move?". InMemoryRegistry is a small
//   RWMutex-guarded map keyed by agent ID; orchestrators snapshot the list
//   on start and use FindByCapability for capability-based dispatch.
//
// Key declarations:
//   - ErrNotFound: sentinel returned by Get when an ID is unknown.
//   - Registry: Register / Get / List / FindByCapability interface.
//   - InMemoryRegistry / NewInMemory: thread-safe in-process registry.
//
// See doc.go for package-level documentation.

package registry
