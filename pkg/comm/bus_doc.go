// bus.go — pub/sub message bus interface and in-memory implementation.
//
// Overview:
//   Defines the Bus abstraction used by the orchestrator to route protocol
//   messages between agents and provides InMemoryBus, a goroutine-dispatched
//   pub/sub bus suitable for single-process demos and tests. Targeted and
//   broadcast subscribers are both supported.
//
// Key declarations:
//   - Handler: function signature for message consumers.
//   - Bus: Subscribe / Publish interface that transports can satisfy.
//   - InMemoryBus: RWMutex-guarded in-process bus.
//   - NewInMemoryBus: constructor returning an empty bus.
//
// See doc.go for package-level documentation.

package comm
