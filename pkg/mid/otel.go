// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/mid/otel.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga.
//
// Bodhyanga modifications vs upstream:
//   - No OTel trace.Tracer dep at the boundary. The real OTel span
//     creation lives in pkg/observability/otel behind a build tag
//     (see AGENTS.md #12). This middleware stamps a request_id (uuid)
//     onto the context so logs + audit + error envelope share a
//     correlator even when the OTel SDK is not compiled in.
//   - Honours an inbound X-Request-ID header if present (idempotent
//     request retries pick up the same ID). Generates one otherwise.
//   - Echoes the request_id on the outbound response as X-Request-ID
//     so clients can include it in support tickets.

package mid

import (
	"context"
	"net/http"

	"github.com/PratikDhanave/bodhyanga/pkg/web"
	"github.com/google/uuid"
)

// HeaderRequestID is the wire-level header name. Documented in
// docs/operations.md so operators can grep the same string across
// logs, traces, and HTTP headers.
const HeaderRequestID = "X-Request-ID"

// Otel returns the middleware. It is named Otel for parity with the
// upstream chain even though Bodhyanga does not pull in the OTel SDK by
// default. When the binary is built with -tags otel the wrapped span
// is also created (in pkg/observability/otel) and the request_id is
// attached as a span attribute.
func Otel() web.Middleware {
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(ctx context.Context, r *http.Request) (web.Encoder, error) {
			reqID := r.Header.Get(HeaderRequestID)
			if reqID == "" {
				reqID = uuid.NewString()
			}
			ctx = web.SetRequestID(ctx, reqID)

			// Echo on response so the client can correlate with support.
			// We reach for the writer via the web context so this lands
			// even when the handler returns a JSON encoder (which sets
			// its own headers later — but stdlib http.ResponseWriter
			// allows header mutations until WriteHeader fires).
			if w := web.GetWriter(ctx); w != nil {
				w.Header().Set(HeaderRequestID, reqID)
			}
			return next(ctx, r)
		}
	}
}
