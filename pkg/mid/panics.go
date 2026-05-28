// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/mid/panics.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh.
//
// Bodh modifications vs upstream:
//   - No metrics package dependency (Bodh metrics counter lives inline in
//     pkg/mid.Metrics).
//   - On panic the middleware emits an errs.InternalOnlyLog error so the
//     downstream Errors middleware logs the trace but redacts it before
//     responding — the client never sees the panic message (which may
//     accidentally contain narrative / stacktrace symbols).
//   - The recovered value is INTENTIONALLY not put into err.Error() of the
//     outbound envelope. AGENTS.md invariant #2 — "never widen the audit
//     log surface to PHI" — extends to the wire surface.

package mid

import (
	"context"
	"net/http"
	"runtime/debug"
	"sync/atomic"

	"github.com/PratikDhanave/bodh/pkg/errs"
	"github.com/PratikDhanave/bodh/pkg/web"
)

// panicCount is a process-wide counter incremented every time the
// recover() fires. Exposed via Panics_Count for the metrics middleware
// to scrape; deliberately not exported as a Prometheus gauge here
// because pkg/mid does not pull in a metrics-client dep.
var panicCount atomic.Uint64

// Panics returns the outermost middleware in the standard chain. It
// recovers from any panic raised by an inner middleware or the handler
// itself, translates the panic into an errs.InternalOnlyLog error, and
// bumps the panic counter.
//
// Why InternalOnlyLog (not Internal): the panic value might be a string
// like "patient 12345 not found" — putting that into the wire response
// would leak the ID to the client. InternalOnlyLog tells the Errors
// middleware "log details, respond with a generic 500".
func Panics() web.Middleware {
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(ctx context.Context, r *http.Request) (resp web.Encoder, err error) {
			defer func() {
				if rec := recover(); rec != nil {
					panicCount.Add(1)
					trace := debug.Stack()
					// We construct the error with Errorf so the call site
					// (FileName / FuncName) reflects the panic site, not
					// the recover site. The stacktrace lives in the error
					// message — the Errors middleware will redact it before
					// any log line is written.
					err = errs.Errorf(errs.InternalOnlyLog, "panic: %v\n%s", rec, string(trace))
					resp = nil
				}
			}()
			return next(ctx, r)
		}
	}
}

// PanicCount returns the number of panics the middleware has caught
// since process start. The Metrics middleware reads this and emits it
// as a counter; tests use it directly.
func PanicCount() uint64 {
	return panicCount.Load()
}

// resetPanicCount is internal — used by tests to isolate counters.
func resetPanicCount() {
	panicCount.Store(0)
}
