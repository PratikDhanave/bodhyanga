// eval.go — InteractionRecord type and pluggable Store interface.
//
// Overview:
//   Defines the trace row used to compare runs over time (scenario, success
//   flag, named metrics, free-form metadata, start/end times) and the Store
//   contract that records persist through. InMemoryStore is a slice-backed
//   reference implementation; the persistence package provides a JSON-file
//   variant.
//
// Key declarations:
//   - InteractionRecord: persisted summary of one interaction or sub-task.
//   - Store: Save / List interface for record backends.
//   - InMemoryStore / NewInMemoryStore: thread-safe in-process implementation.
//
// See doc.go for package-level documentation.

package eval
