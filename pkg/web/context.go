// Adapted from https://github.com/ardanlabs/service/blob/master/foundation/web/context.go
// Original copyright Ardan Labs (Apache 2.0). Modifications for Bodh.
//
// Bodh modifications vs upstream:
//   - The OTel tracer key is gone. Bodh's tracer lives in
//     pkg/observability/otel behind a build tag; the request-scoped
//     identifier propagated through context here is the RequestID
//     (string), set by pkg/mid.Otel.
//   - RequestID is the load-bearing correlator. Every audit event,
//     every log line, every error envelope carries it.

package web

import (
	"context"
	"net/http"
)

type ctxKey int

const (
	writerKey ctxKey = iota + 1
	requestIDKey
	startKey
)

// setWriter stashes the http.ResponseWriter on the request context so raw
// handlers (file servers, SSE) can pull it back out. Internal — handlers
// should not use the writer directly; return an Encoder instead.
func setWriter(ctx context.Context, w http.ResponseWriter) context.Context {
	return context.WithValue(ctx, writerKey, w)
}

// SetWriter is the test seam for middleware that wants to attach a
// response writer onto a context outside the App.ServeHTTP path. Not
// part of the production handler contract — handlers must NOT call this.
func SetWriter(ctx context.Context, w http.ResponseWriter) context.Context {
	return setWriter(ctx, w)
}

// GetWriter returns the request's http.ResponseWriter, or nil if the
// context was not created by the web framework. Use sparingly — most
// handlers should not need it.
func GetWriter(ctx context.Context) http.ResponseWriter {
	if v, ok := ctx.Value(writerKey).(http.ResponseWriter); ok {
		return v
	}
	return nil
}

// SetRequestID stamps a request-scoped correlation ID onto the context.
// Called by pkg/mid.Otel (or any compatible middleware) before the
// handler runs.
func SetRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// GetRequestID returns the request-scoped correlation ID, or "" if not
// set. The Errors middleware stamps this onto every outbound *errs.Error
// so clients see request_id in the envelope.
func GetRequestID(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// setStart marks the moment the request entered the middleware chain.
// The Logger middleware reads it to emit the request duration.
func setStart(ctx context.Context, start int64) context.Context {
	return context.WithValue(ctx, startKey, start)
}

// GetStart returns the request-start unix-nano timestamp, or 0 if not set.
func GetStart(ctx context.Context) int64 {
	if v, ok := ctx.Value(startKey).(int64); ok {
		return v
	}
	return 0
}
