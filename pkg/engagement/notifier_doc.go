// notifier.go — in-memory outreach notifier used by the engagement agent.
//
// Overview:
//   Logs SMS / email / IVR messages without hitting external services so tests
//   and demos can assert what would have been sent. Supports acknowledgement
//   tracking and an adherence-rate helper, plus a stock formatter for
//   care-gap outreach messages.
//
// Key declarations:
//   - Channel / ChannelSMS, ChannelEmail, ChannelIVR: delivery mechanism enum.
//   - Notification: one outreach record (sent + optionally acked).
//   - Notifier: Send / List / Ack interface that production fan-outs implement.
//   - InMemoryNotifier / NewInMemory: thread-safe in-process recorder.
//   - InMemoryNotifier.AdherenceRate: fraction of sent notifications acked.
//   - FormatGapMessage: standard care-gap outreach body.
//
// See doc.go for package-level documentation.

package engagement
