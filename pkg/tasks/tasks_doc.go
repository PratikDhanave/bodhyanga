// tasks.go — in-memory clinical task queue for nurse/staff workflows.
//
// Overview:
//   Defines the NurseTask record (refills, prior auths, TCM follow-ups, inbox
//   items, gap outreach) and a thread-safe Queue that the Virtual Office
//   agents create, list, filter, and update during a session.
//
// Key declarations:
//   - Status / StatusOpen, StatusInProgress, StatusDone, StatusCancelled: task lifecycle states.
//   - NurseTask: one unit of clinical work with priority, due date, and assignment.
//   - Queue / NewQueue: thread-safe in-memory store keyed by task ID.
//   - Queue.Create, Queue.UpdateStatus, Queue.List, Queue.FilterOpen, Queue.CountByKind: CRUD and analytics operations.
//
// See doc.go for package-level documentation.

package tasks
