// Adapted from https://github.com/ardanlabs/service/blob/master/foundation/web/middleware.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh.
//
// Bodh modifications vs upstream:
//   - Type renamed from MidFunc to Middleware to read better at call sites
//     (`mid.Panics()` returns a Middleware, plain English).
//   - SetStart helper is exported so the Logger middleware can stamp the
//     request-start timestamp without reaching into the context-key
//     internals.

package web

import (
	"context"
	"time"
)

// Middleware is a function that wraps a HandlerFunc to add cross-cutting
// behaviour: panic recovery, request logging, metrics, tracing, error
// translation. Middlewares compose via wrapMiddleware below.
type Middleware func(HandlerFunc) HandlerFunc

// wrapMiddleware composes a slice of middlewares around a base handler.
// Middlewares run in slice order on the inbound side, reverse order on
// the outbound side. The standard Bodh chain is:
//
//	mid.Panics() → mid.Logger(logger) → mid.Metrics() → mid.Otel(tracer) → mid.Errors() → handler
//
// so Panics is outermost (catches a panic in any other middleware) and
// Errors is innermost (last chance to translate a handler error into
// the wire envelope before the response is written).
func wrapMiddleware(mw []Middleware, handler HandlerFunc) HandlerFunc {
	for i := len(mw) - 1; i >= 0; i-- {
		if m := mw[i]; m != nil {
			handler = m(handler)
		}
	}
	return handler
}

// WrapMiddleware exports the composition helper so callers outside the
// package (tests, integration code) can build ad-hoc chains without
// going through App.HandlerFunc.
func WrapMiddleware(mw []Middleware, handler HandlerFunc) HandlerFunc {
	return wrapMiddleware(mw, handler)
}

// StampStart attaches the request-start timestamp to the context. The
// Logger middleware calls this on entry and Duration on the outbound
// side. Exposed so non-Logger middlewares can also read request age
// without reimplementing the seam.
func StampStart(ctx context.Context) context.Context {
	return setStart(ctx, time.Now().UnixNano())
}

// Duration returns time elapsed since the request started, computed
// from the timestamp stamped by StampStart. Returns 0 if no stamp was
// set (e.g. a unit test that bypassed the middleware chain).
func Duration(ctx context.Context) time.Duration {
	start := GetStart(ctx)
	if start == 0 {
		return 0
	}
	return time.Duration(time.Now().UnixNano() - start)
}
