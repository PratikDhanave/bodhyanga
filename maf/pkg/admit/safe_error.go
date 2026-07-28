package admit

import (
	"errors"
	"fmt"
)

// AuditSafe marks an error whose Error() text is guaranteed free of PHI and
// message payload, and may therefore be copied verbatim into an audit event.
//
// Errors that do NOT implement AuditSafe are redacted to a type-only
// descriptor by SafeErrorText before they can reach the trail. This turns the
// "zero PHI in audit events" rule from an upstream discipline — every error
// constructor must remember never to interpolate payload — into a property
// enforced at the sink: redaction is the default, and passing text through is
// the deliberate, opt-in exception (see intake.RejectError, agents.auditSafeErr).
type AuditSafe interface {
	// AuditSafe is a marker; implementing it asserts Error() is PHI-free.
	AuditSafe()
}

// SafeErrorText returns the message of the first AuditSafe error in err's
// chain; when none is present it returns a redacted descriptor naming only the
// error's Go type, never its message. A nil error yields "".
//
// The verbatim path deliberately uses the matched safe error's own text rather
// than err.Error(), so an unsafe outer wrapper (fmt.Errorf("...%w", safeErr))
// cannot smuggle payload past the guard.
func SafeErrorText(err error) string {
	if err == nil {
		return ""
	}
	var safe AuditSafe
	if errors.As(err, &safe) {
		if e, ok := safe.(error); ok {
			return e.Error()
		}
	}
	return fmt.Sprintf("redacted error (%T)", err)
}
