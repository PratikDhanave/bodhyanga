// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/mid/errors.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh.
//
// Bodh modifications vs upstream:
//   - The middleware logs through pkg/phi.LoggerWrapper, never the raw
//     observability.Logger. This is the AGENTS.md invariant #2 line: the
//     audit log surface stays clean of err.Error() strings whose wrapped
//     chain might contain a chief complaint or HPI narrative.
//   - When the inner error is NOT an *errs.Error, the middleware does
//     NOT echo err.Error() into the wire envelope. It returns a generic
//     "internal error" message and logs the typed details under a
//     redacted summary. Untyped errors are by definition out-of-contract
//     and may contain anything; assume PHI.
//   - When the typed error code is InternalOnlyLog the message is
//     replaced with a generic string before responding (matches
//     upstream behaviour, but the redaction step is explicit).
//   - The request_id from the context is stamped onto the response
//     envelope so clients can include it in support tickets.

package mid

import (
	"context"
	"errors"
	"net/http"
	"path"

	"github.com/PratikDhanave/bodh/pkg/errs"
	"github.com/PratikDhanave/bodh/pkg/observability"
	"github.com/PratikDhanave/bodh/pkg/phi"
	"github.com/PratikDhanave/bodh/pkg/web"
)

// Errors returns the innermost middleware in the standard chain — last
// chance to translate a handler error into the wire envelope.
//
// Wiring contract: the supplied raw logger is wrapped in a phi.LoggerWrapper
// before any line is written. Even though the typed envelope tries hard
// not to carry PHI, defense in depth — the redactor still runs at the seam.
func Errors(raw observability.Logger) web.Middleware {
	wrapped := &phi.LoggerWrapper{Redactor: phi.NewRedactor(), Inner: raw}
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(ctx context.Context, r *http.Request) (web.Encoder, error) {
			resp, err := next(ctx, r)
			if err == nil {
				return resp, nil
			}

			// Try to extract the typed envelope. If the handler returned
			// a plain error (out-of-contract) we wrap it as generic
			// Internal — the wrapped chain is INTENTIONALLY NOT echoed
			// into the response message (it might contain PHI).
			var appErr *errs.Error
			if !errors.As(err, &appErr) {
				appErr = errs.Errorf(errs.Internal, "internal error")
				// Log the original error type + redacted shape so we
				// can debug it from operator logs without leaking it
				// to the API caller. We pre-format the error to a string
				// so the LoggerWrapper's redactor sees a string arg
				// (it only redacts strings — see pkg/phi/redact.go).
				wrapped.Printf("[care] untyped handler error: %s", err.Error())
			}

			// Log the typed envelope. FileName / FuncName came from the
			// errs.New call site so operators can grep to the line.
			wrapped.Printf("[care] handler error code=%s message=%q file=%s func=%s request_id=%s",
				appErr.Code.String(),
				appErr.Message,
				path.Base(appErr.FileName),
				path.Base(appErr.FuncName),
				web.GetRequestID(ctx))

			// InternalOnlyLog: the typed code that says "log but never
			// echo the message." Used by the Panics middleware so the
			// stack trace doesn't escape to the client.
			if appErr.Code.Equal(errs.InternalOnlyLog) {
				appErr = errs.Errorf(errs.Internal, "internal error")
			}

			// Stamp request_id into the envelope so the client sees the
			// same correlator that appears in the X-Request-ID header.
			appErr = appErr.WithRequestID(web.GetRequestID(ctx))

			// Return the envelope as the response; nil error so the
			// outer wrapper writes the body via Respond.
			return appErr, nil
		}
	}
}

// Compile-time assertion that *errs.Error satisfies web.Encoder. Without
// this the middleware silently returns a value the framework can't write.
var _ web.Encoder = (*errs.Error)(nil)
