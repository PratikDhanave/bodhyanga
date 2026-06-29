package gateway

import (
	"context"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// Middleware is an extra admission gate that runs on the allow path, after
// governance and before the agent. It is the extension point for the
// defence-in-depth checks the decoupling plan calls for — content safety, PII
// scrubbing, circuit breaking, output validation — without changing the core
// governance+audit Pipeline.
//
// Contract:
//   - Check runs synchronously on the message hot path; keep it fast.
//   - admit=false blocks the message (the Pipeline audits the block as a
//     policy_decision deny and does NOT invoke the agent).
//   - A non-nil error is treated as a block (fail closed) — the safer posture
//     than admitting a message a gate could not evaluate.
//   - reason is a short PHI-free string surfaced in the audit event and logs.
type Middleware interface {
	// Name identifies the middleware in audit reasons and logs (e.g.
	// "content_safety", "circuit_breaker").
	Name() string
	// Check reports whether the message may proceed.
	Check(ctx context.Context, msg protocol.Message) (admit bool, reason string, err error)
}

// FuncMiddleware adapts a plain function into a Middleware, so callers can add
// an ad-hoc gate without declaring a type. The zero value is not usable; set
// both fields.
type FuncMiddleware struct {
	// Label is returned by Name.
	Label string
	// Fn implements Check.
	Fn func(ctx context.Context, msg protocol.Message) (admit bool, reason string, err error)
}

// Name returns the configured label.
func (f FuncMiddleware) Name() string { return f.Label }

// Check delegates to Fn.
func (f FuncMiddleware) Check(ctx context.Context, msg protocol.Message) (bool, string, error) {
	return f.Fn(ctx, msg)
}

// DefaultClinicalTypes returns the message types that receive an audit event
// on the ALLOW path, mirroring pkg/orchestration's clinicalMessageTypes. The
// returned map is a fresh copy the caller may keep or mutate. Keeping this in
// lock-step with the orchestrator is what makes a decoupled agent's audit
// trail identical to the monolith's.
func DefaultClinicalTypes() map[string]bool {
	return map[string]bool{
		"presentation":     true,
		"case_state":       true,
		"ask_question":     true,
		"order_test":       true,
		"diagnosis":        true,
		"verify_reasoning": true,
	}
}
