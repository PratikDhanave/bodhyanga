// queue_test.go — unit tests for the HITL review queue.
//
// Overview:
//   Verifies the queue's lifecycle: that submissions land as pending, that
//   approvals/rejections are recorded once, that invalid decisions are
//   rejected, and that Expire correctly transitions overdue pending
//   requests while leaving in-budget and SLA-less requests alone.
//
// Key declarations:
//   - TestQueueLifecycle: covers Submit / Decide / Pending / CountByStatus / CountByKind.
//   - TestQueueExpire: validates SLA-driven expiration of pending reviews.
//
// See doc.go for package-level documentation.

package hitl
