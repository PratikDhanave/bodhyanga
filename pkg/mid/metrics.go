// Adapted from https://github.com/ardanlabs/service/blob/master/app/sdk/mid/metrics.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodhyanga.
//
// Bodhyanga modifications vs upstream:
//   - Pure process-local atomic counters. No expvar dep, no Prometheus
//     dep (those live in pkg/observability behind build tags). Operators
//     who need scrapeable metrics export the snapshot via the /api/admin
//     endpoint already exposed by cmd/care.
//   - The handler-return is inspected ONLY to bump the error counter;
//     the error itself is not formatted or logged here. That keeps the
//     metrics middleware completely PHI-free, no LoggerWrapper required.

package mid

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/PratikDhanave/bodhyanga/pkg/errs"
	"github.com/PratikDhanave/bodhyanga/pkg/web"
)

// requestCount + errorCount are process-wide atomic counters. The
// MetricsSnapshot helper returns a point-in-time read for the admin
// endpoint and tests.
var (
	requestCount atomic.Uint64
	errorCount   atomic.Uint64
)

// MetricsSnapshot is the read-side projection of the counters.
type MetricsSnapshot struct {
	Requests uint64 `json:"requests"`
	Errors   uint64 `json:"errors"`
	Panics   uint64 `json:"panics"`
}

// Snapshot returns the current counter values. Safe to call from any
// goroutine.
func Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		Requests: requestCount.Load(),
		Errors:   errorCount.Load(),
		Panics:   PanicCount(),
	}
}

// Metrics returns the middleware. It increments the requests counter on
// every inbound request and the errors counter on every non-nil handler
// return. The counters are independent of HTTP status code (a 4xx
// validation error still bumps errors) — that's the right granularity
// for "how many handler invocations went sideways."
func Metrics() web.Middleware {
	return func(next web.HandlerFunc) web.HandlerFunc {
		return func(ctx context.Context, r *http.Request) (web.Encoder, error) {
			requestCount.Add(1)
			resp, err := next(ctx, r)
			if err != nil {
				// Don't double-count panic errors — the Panics middleware
				// already bumped panicCount, and Errors will see the same
				// err. Just one increment per err.
				var appErr *errs.Error
				if errors.As(err, &appErr) && appErr.Code.Equal(errs.InternalOnlyLog) {
					// Skipped: counted under panics already.
				} else {
					errorCount.Add(1)
				}
			}
			return resp, err
		}
	}
}

// resetCounters is internal — for test isolation.
func resetCounters() {
	requestCount.Store(0)
	errorCount.Store(0)
	resetPanicCount()
}
