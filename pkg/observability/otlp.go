// otlp.go — real OTLP/HTTP exporter for LLMTrace records.
//
// Build tag: this file is compiled ONLY when the `otel` build tag is set
// (e.g. `go build -tags otel ./cmd/care`). The default Bodhyanga build does not
// link the OTel SDK — see otlp_stub.go for the no-op shim used otherwise.
// Gating keeps go.sum tiny (~1 dep) for users who don't need OTLP export.
//
// Wiring (from cmd/*/main.go, also under //go:build otel):
//
//	exp, err := observability.NewOTLPLLMExporter(ctx, "http://localhost:4318")
//	if err != nil { log.Fatal(err) }
//	observability.SetLLMOTelHook(exp.Record)
//	defer exp.Shutdown(context.Background())
//
// PHI policy: span attributes never include prompt/response content. The
// CaseID is hashed (sha256, first 16 hex chars) before being attached so a
// real MRN can never leak into a collector backend.

//go:build otel

package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// OTLPLLMExporter ships LLMTrace records to an OTLP/HTTP collector as spans.
// Construct via NewOTLPLLMExporter and install on the global hook with
// observability.SetLLMOTelHook(exp.Record). Always call Shutdown on
// shutdown to flush pending spans.
type OTLPLLMExporter struct {
	tp     *sdktrace.TracerProvider
	tracer trace.Tracer
}

// NewOTLPLLMExporter builds an exporter pointed at the given OTLP/HTTP
// endpoint (e.g. "http://localhost:4318"). It returns a started exporter
// ready to receive Record calls. The service name is "bodhyanga-care".
func NewOTLPLLMExporter(ctx context.Context, endpoint string) (*OTLPLLMExporter, error) {
	host, path, insecure, err := splitOTLPEndpoint(endpoint)
	if err != nil {
		return nil, fmt.Errorf("otlp endpoint: %w", err)
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
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("bodhyanga-care"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(2*time.Second),
		),
		sdktrace.WithResource(res),
	)

	// Set as the process-wide global so that otelhttp (TraceHandler) and any
	// otel.Tracer() call elsewhere in the binary can use the same provider
	// without needing a reference threaded through the call stack.
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &OTLPLLMExporter{
		tp:     tp,
		tracer: tp.Tracer("github.com/PratikDhanave/bodhyanga/pkg/observability"),
	}, nil
}

// Record converts an LLMTrace into a span and queues it for OTLP export.
// Span name is "llm.<kind>"; the span starts at t.StartedAt and ends
// t.DurationMs later. Errors on the trace become span errors. PHI is
// never attached — CaseID is hashed, prompt/response content is dropped.
func (e *OTLPLLMExporter) Record(t LLMTrace) {
	if e == nil || e.tracer == nil {
		return
	}
	kind := t.Kind
	if kind == "" {
		kind = "completion"
	}
	start := t.StartedAt
	if start.IsZero() {
		start = time.Now().Add(-time.Duration(t.DurationMs) * time.Millisecond)
	}
	end := start.Add(time.Duration(t.DurationMs) * time.Millisecond)

	attrs := []attribute.KeyValue{
		attribute.String("provider", t.Provider),
		attribute.String("model", t.Model),
		attribute.String("kind", kind),
		attribute.Int("input_tokens", t.InputTokens),
		attribute.Int("output_tokens", t.OutputTokens),
		attribute.Int("cached_tokens", t.CachedTokens),
	}
	if t.AgentID != "" {
		attrs = append(attrs, attribute.String("agent_id", t.AgentID))
	}
	if t.CaseID != "" {
		// Hash to avoid leaking MRNs. 16 hex chars is enough to correlate
		// spans across a case without being reversible.
		attrs = append(attrs, attribute.String("case_id", hashID(t.CaseID)))
	}
	if t.TraceID != "" {
		attrs = append(attrs, attribute.String("trace_id", t.TraceID))
	}
	if t.StopReason != "" {
		attrs = append(attrs, attribute.String("stop_reason", t.StopReason))
	}
	if t.ToolCalls > 0 {
		attrs = append(attrs, attribute.Int("tool_calls", t.ToolCalls))
	}

	_, span := e.tracer.Start(context.Background(),
		"llm."+kind,
		trace.WithTimestamp(start),
		trace.WithAttributes(attrs...),
	)
	if t.Error != "" {
		span.RecordError(fmt.Errorf("%s", t.Error))
	}
	span.End(trace.WithTimestamp(end))
}

// Shutdown flushes pending spans and tears down the tracer provider.
// Callers should pass a context with a sensible deadline.
func (e *OTLPLLMExporter) Shutdown(ctx context.Context) error {
	if e == nil || e.tp == nil {
		return nil
	}
	return e.tp.Shutdown(ctx)
}

// splitOTLPEndpoint parses "http://host:4318" into host+port, optional path,
// and an insecure flag. The OTLP HTTP exporter's WithEndpoint takes only
// host:port (no scheme), with a separate URLPath option and an insecure
// boolean. Defaults: insecure=false for https://, true for http:// or bare
// host:port.
func splitOTLPEndpoint(endpoint string) (host, path string, insecure bool, err error) {
	if !strings.Contains(endpoint, "://") {
		// Bare host:port — treat as insecure (loopback dev collector).
		return endpoint, "", true, nil
	}
	u, perr := url.Parse(endpoint)
	if perr != nil {
		return "", "", false, perr
	}
	host = u.Host
	path = strings.TrimRight(u.Path, "/")
	insecure = u.Scheme == "http"
	return host, path, insecure, nil
}

// hashID returns a short, stable, non-reversible identifier for a CaseID
// so spans can be correlated without leaking MRNs.
func hashID(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
