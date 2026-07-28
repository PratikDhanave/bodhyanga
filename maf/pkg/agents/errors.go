package agents

// auditSafeErr is a static, PHI-free error whose text is safe to record in the
// audit trail. It satisfies admit.AuditSafe structurally (the marker method),
// so admit.SafeErrorText passes its message through instead of redacting it.
//
// Use it only for messages built from constants and non-PHI decision data
// (e.g. a confidence value) — never from case content. Errors that wrap an
// external cause (a provider call, a parser) stay plain fmt.Errorf and are
// redacted at the sink.
type auditSafeErr string

func (e auditSafeErr) Error() string { return string(e) }

// AuditSafe marks the message as PHI-free (see admit.AuditSafe).
func (auditSafeErr) AuditSafe() {}
