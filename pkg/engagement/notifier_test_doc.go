// notifier_test.go — unit tests for the in-memory engagement notifier.
//
// Overview:
//   Confirms that notifications are stored in send order, that acking flips a
//   record and updates the adherence rate accordingly, and that the
//   care-gap message formatter produces a non-empty string.
//
// Key declarations:
//   - TestNotifierSendAndAdherence: exercises Send / List / Ack and AdherenceRate math.
//   - TestFormatGapMessage: smoke test for the FormatGapMessage helper.
//
// See doc.go for package-level documentation.

package engagement
