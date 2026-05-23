// tasks_test.go — unit tests for the in-memory nurse-task queue.
//
// Overview:
//   Exercises the Queue lifecycle: creation with auto-ID, due-date ordering on
//   List, status transitions, filtering of open tasks, and the CountByKind
//   histogram used by analytics dashboards.
//
// Key declarations:
//   - TestQueueLifecycle: drives Create / List / UpdateStatus / FilterOpen / CountByKind across multiple task kinds.
//
// See doc.go for package-level documentation.

package tasks
