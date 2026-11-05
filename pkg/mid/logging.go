// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/mid/logging.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga.
//
// Bodhyanga modifications vs upstream:
//   - The logger is wrapped by pkg/phi.LoggerWrapper, so headers / path /
//     remoteaddr go through the same redactor that protects every other
//     log seam in Bodhyanga. AGENTS.md invariant: PHI never reaches an
//     unredacted log path.
//   - Only a curated allow-list of headers is recorded (no Authorization,
//     no Cookie, no X-Forwarded-For unless explicitly safe). Catch-all
//     header logging would leak bearer tokens to the audit pipeline.
//   - The "completed" log line carries the typed error code from the
//     handler return — not the err.Error() message. This is the seam
//     where the audit log learns whether a 500 was the panic counter,
//     the policy denial counter, or a real internal fault, WITHOUT
//     learning anything about the request body.

package mid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/errs"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
	"github.com/PratikDhanave/bodhyanga/pkg/phi"
	"github.com/PratikDhanave/bodhyanga/pkg/web"
)

// Logger returns a middleware that emits two structured-ish log lines per
// request: "request started" on inbound and "request completed" with
// duration + status on outbound.
//
// The supplied raw observability.Logger is wrapped in a phi.LoggerWrapper
// before any line is written. Callers MAY pass an already-wrapped logger;
// double-wrapping is a no-op cost-wise (the inner Redactor is idempotent
// on already-redacted text) so no defensive unwrap is needed.
//
// Headers logged: only Host + User-Agent + X-Request-ID. Adding more here
// is a wire-surface change; document it in docs/operations.md first.
func Logger(raw observability.Logger) web.Middleware {
	wrapped := &phi.LoggerWrapper{Redactor: phi.NewRedactor(), Inner: raw}
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(ctx context.Context, r *http.Request) (web.Encoder, error) {
			ctx = web.StampStart(ctx)
			path := r.URL.Path
			if r.URL.RawQuery != "" {
				path = fmt.Sprintf("%s?%s", path, r.URL.RawQuery)
			}
			reqID := web.GetRequestID(ctx)
			ua := r.Header.Get("User-Agent")

			// Inbound log line. Format chosen for grep-ability; switch to
			// structured logging when the codebase adopts slog wholesale.
			wrapped.Printf("[care] request started method=%s path=%s host=%s user_agent=%q request_id=%s",
				r.Method, path, r.Host, ua, reqID)

			resp, err := next(ctx, r)

			// Translate the handler's return into a code without ever
			// logging the message (which may contain redactable shapes
			// the LoggerWrapper would catch, but better to never let it
			// get that close to the seam).
			codeName := errs.None.String()
			if err != nil {
				codeName = errs.Internal.String()
				var appErr *errs.Error
				if errors.As(err, &appErr) {
					codeName = appErr.Code.String()
				}
			}
			wrapped.Printf("[care] request completed method=%s path=%s code=%s duration=%s request_id=%s",
				r.Method, path, codeName, time.Duration(time.Now().UnixNano()-startNanos(ctx)).String(), reqID)
			return resp, err
		}
	}
}

// startNanos is a helper that lets us compute duration without exposing
// the context key. Returns now() if the request was never stamped (the
// duration will read 0).
func startNanos(ctx context.Context) int64 {
	if s := web.GetStart(ctx); s != 0 {
		return s
	}
	return time.Now().UnixNano()
}
