// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// trace_handler_stub.go — passthrough shim for the default (non-otel) build.
//
// When the binary is built without -tags otel, TraceHandler is a no-op
// wrapper that returns next unchanged. This keeps callers in cmd/care
// unconditional — no build-tag guards in main.go.

//go:build !otel

package observability

import (
	"context"
	"errors"
	"net/http"
)

// TraceHandler returns next unchanged. Build with -tags otel for real
// OpenTelemetry HTTP span instrumentation.
func TraceHandler(next http.Handler, _ string) http.Handler {
	return next
}

// InitGlobalTracer is a no-op in the default build. It returns an error so
// callers know the SDK is not linked, and a no-op shutdown function.
func InitGlobalTracer(_ context.Context, _ string) (func(context.Context) error, error) {
	noop := func(_ context.Context) error { return nil }
	return noop, errors.New("observability: OTel tracer requires the 'otel' build tag")
}
