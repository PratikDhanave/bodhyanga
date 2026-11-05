// Copyright 2025 The Bodhyanga Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// OTel HTTP trace handler — real implementation (requires -tags otel).
//
// TraceHandler wraps an http.Handler with OpenTelemetry HTTP instrumentation
// from go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp. Every
// incoming request becomes an OTel span. Span names are "<METHOD> <path>".
//
// Prerequisite: the global TracerProvider must be set (via
// otel.SetTracerProvider) before the handler starts receiving requests.
// NewOTLPLLMExporter does this automatically when called at startup; if you
// enable tracing without the LLM exporter, call InitGlobalTracer explicitly.
//
// W3C TraceContext propagation (traceparent / tracestate headers) is
// installed by InitGlobalTracer so the care API can participate in
// distributed traces from an upstream gateway.
//
// Build: go build -tags otel ./cmd/care

//go:build otel

package observability

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// TraceHandler wraps next with OTel HTTP span instrumentation. Each request
// becomes a span named "operation" (usually the service name, e.g.
// "bodhyanga-care"); otelhttp adds http.method, http.route, http.status_code as
// span attributes automatically.
//
// Spans are exported by the global TracerProvider; call InitGlobalTracer (or
// NewOTLPLLMExporter, which calls it internally) before registering routes.
func TraceHandler(next http.Handler, operation string) http.Handler {
	return otelhttp.NewHandler(next, operation,
		otelhttp.WithTracerProvider(otel.GetTracerProvider()),
		otelhttp.WithPropagators(otel.GetTextMapPropagator()),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + normalizePath(r.URL.Path)
		}),
	)
}

// InitGlobalTracer sets up the global OTel TracerProvider and W3C
// TraceContext propagator. It is called automatically by NewOTLPLLMExporter;
// call it directly only if you want HTTP span tracing without LLM span export.
//
// Returns a shutdown function the caller must invoke on process exit.
func InitGlobalTracer(ctx context.Context, endpoint string) (func(context.Context) error, error) {
	host, path, insecure, err := splitOTLPEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("tracer: %w", err)
	}

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(host)}
	if path != "" {
		opts = append(opts, otlptracehttp.WithURLPath(path))
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("tracer exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName("bodhyanga-care")),
	)
	if err != nil {
		return nil, fmt.Errorf("tracer resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)

	// Set as the global provider so otelhttp + user code can call
	// otel.Tracer(...) without threading a provider through the call stack.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// Tracer returns a named tracer from the global provider. Use this in
// application code that wants to add spans without importing the full OTel SDK.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
