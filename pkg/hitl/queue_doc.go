// queue.go — human-in-the-loop review queue with submit / decide / expire semantics.
//
// Overview:
//   Backs the HITL gate. Clinical actions that require a clinician's approval
//   are stored here as medical.ReviewRequest entries; reviewers decide
//   (approve / approve_with_modifications / reject) and the gate agent
//   resumes the original action. Pending requests past their SLA can be
//   bulk-expired by a scheduler.
//
// Key declarations:
//   - Queue / NewQueue: thread-safe in-memory review queue.
//   - Queue.Submit: enqueue a new request as pending.
//   - Queue.Decide: record a reviewer decision; rejects invalid or already-decided IDs.
//   - Queue.Pending, Queue.List, Queue.Get: query helpers.
//   - Queue.CountByKind, Queue.CountByStatus: histograms for dashboards.
//   - Queue.Expire: transition past-SLA pending requests to expired.
//
// See doc.go for package-level documentation.

package hitl
