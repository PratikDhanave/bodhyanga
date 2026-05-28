// Package errs provides typed error codes and the Bodh error envelope used
// by every HTTP handler that goes through pkg/web.
//
// Adapted from Bill Kennedy / Ardan Labs' Ultimate Service template
// (https://github.com/ardanlabs/service), licensed Apache 2.0. See the
// NOTICE and LICENSE-APACHE files at the repository root. Bodh extends the
// upstream codes with HITLPending, SafetyRefusal, and PolicyDenied (the
// Bodh-specific categories), and changes the wire envelope to
// {code, message, fields, request_id} so error responses carry the
// request_id that pkg/mid.Otel propagates.
//
// Why typed codes (not raw HTTP statuses): handlers should describe what
// went wrong in domain terms, not in HTTP terms. Two handlers returning
// errs.NotFound both produce HTTP 404, but only one of them ever has to
// know what 404 is. This keeps the handler signature stable when the wire
// protocol changes (gRPC, server-sent events, etc).
//
// PHI invariants (see AGENTS.md):
//   - The Error.Message field is sent back to the client. Never put PHI
//     into it. Pass a redacted summary instead and let the audit log
//     carry the IDs.
//   - The Error.Error() method returns Message only — wrapped error chains
//     are not echoed. If a handler wraps `pkg/medical` errors that might
//     contain a chief complaint, it MUST use Errorf with a hand-built
//     message rather than New(code, wrapped).
package errs
