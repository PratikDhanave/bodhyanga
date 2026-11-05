// laminar.go — Laminar (lmnr.ai) OTLP/HTTP exporter for LLMTrace records.
//
// Build tag: this file is compiled ONLY when the `otel` build tag is set
// (e.g. `go build -tags otel ./cmd/care`). The default Bodhyanga build does not
// link the OTel SDK — see laminar_stub.go for the no-op shim used otherwise.
//
// Laminar is an LLM observability backend that ingests OTLP spans. It has no
// Go SDK; the integration is standard OpenTelemetry with two additions:
//
//   - OTLP endpoint: api.lmnr.ai:443 (HTTPS), path /v1/traces
//   - Authorization header: "Bearer <LMNR_PROJECT_API_KEY>"
//   - Laminar-specific span attributes: lmnr.span.type=LLM + gen_ai.* semconv
//
// Wiring (cmd/*/main.go, also behind //go:build otel):
//
//	exp, err := observability.NewLaminarExporter(ctx)
//	if err != nil { log.Fatal(err) }
//	observability.SetLLMOTelHook(exp.Record)
//	defer exp.Shutdown(context.Background())
//
// Environment variables:
//
//	LMNR_PROJECT_API_KEY  — Laminar project API key (required for cloud)
//	LMNR_BASE_URL         — override OTLP endpoint (default: https://api.lmnr.ai)
//	                        set to "http://localhost:8000" for self-hosted
//	BODHYANGA_SERVICE_NAME     — OTel service.name attribute (default: "bodhyanga")
//
// PHI policy: same as OTLPLLMExporter — CaseID is hashed (sha256, first 16
// hex chars), prompt/response content is never attached.

//go:build otel

package observability

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	laminarCloudEndpoint = "api.lmnr.ai:443"
	laminarTracesPath    = "/v1/traces"

	// attrLaminarSpanType is the Laminar attribute that marks a span as an LLM
	// call in the Laminar timeline and cost analytics.
	attrLaminarSpanType = "lmnr.span.type"
	attrLaminarSpanLLM  = "LLM"
)

// LaminarExporter ships LLMTrace records to Laminar (lmnr.ai) as OTLP/HTTP
// spans with gen_ai.* semantic conventions and the lmnr.span.type=LLM marker
// that Laminar uses to recognise LLM calls in its timeline and cost views.
//
// Construct via NewLaminarExporter; install with SetLLMOTelHook(exp.Record).
// Always call Shutdown on process exit to flush pending spans.
type LaminarExporter struct {
	tp     *sdktrace.TracerProvider
	tracer trace.Tracer
}

// NewLaminarExporter builds an exporter pointed at the Laminar OTLP endpoint.
// It reads configuration from environment variables:
//
//   - LMNR_PROJECT_API_KEY — API key for Laminar cloud (required unless using
//     a self-hosted instance that accepts unauthenticated traffic)
//   - LMNR_BASE_URL        — override the base URL (default: https://api.lmnr.ai)
//   - BODHYANGA_SERVICE_NAME    — OTel service.name (default: "bodhyanga")
//
// Returns an error if the OTLP exporter cannot connect or the resource cannot
// be built.
func NewLaminarExporter(ctx context.Context) (*LaminarExporter, error) {
	apiKey := os.Getenv("LMNR_PROJECT_API_KEY")
	baseURL := os.Getenv("LMNR_BASE_URL")
	if baseURL == "" {
		baseURL = "https://" + laminarCloudEndpoint
	}
	svcName := os.Getenv("BODHYANGA_SERVICE_NAME")
	if svcName == "" {
		svcName = "bodhyanga"
	}

	host, urlPath, insecure, err := splitOTLPEndpoint(baseURL)
	if err != nil {
		return nil, fmt.Errorf("laminar: invalid endpoint %q: %w", baseURL, err)
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
	}
	// Laminar's cloud uses /v1/traces; self-hosted may differ. Use the path
	// from the URL if provided, otherwise default to the standard OTLP path.
	tracePath := urlPath
	if tracePath == "" {
		tracePath = laminarTracesPath
	}
	opts = append(opts, otlptracehttp.WithURLPath(tracePath))

	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if apiKey != "" {
		opts = append(opts, otlptracehttp.WithHeaders(map[string]string{
			"Authorization": "Bearer " + apiKey,
		}))
	}

	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("laminar: OTLP exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(svcName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("laminar: resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(2*time.Second),
		),
		sdktrace.WithResource(res),
	)

	return &LaminarExporter{
		tp:     tp,
		tracer: tp.Tracer("github.com/PratikDhanave/bodhyanga/pkg/observability/laminar"),
	}, nil
}

// Record converts an LLMTrace into a span with Laminar's expected attributes
// and queues it for OTLP export to lmnr.ai.
//
// Span naming: AgentID when set (groups calls by agent in Laminar's timeline),
// otherwise "llm.<kind>".
//
// Attributes set:
//
//	lmnr.span.type             = "LLM"   (Laminar recognition marker)
//	gen_ai.system              = provider name (e.g. "anthropic")
//	gen_ai.request.model       = model name
//	gen_ai.usage.input_tokens  = int
//	gen_ai.usage.output_tokens = int
//	gen_ai.usage.cache_read_input_tokens = int (when CachedTokens > 0)
//	gen_ai.response.finish_reasons = stop reason (when set)
//	bodhyanga.agent_id              = AgentID (when set)
//	bodhyanga.case_id               = sha256(CaseID)[:16] (when set; never raw)
//	bodhyanga.trace_id              = TraceID (when set)
//	bodhyanga.tool_calls            = int (when > 0)
func (e *LaminarExporter) Record(t LLMTrace) {
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

	spanName := t.AgentID
	if spanName == "" {
		spanName = "llm." + kind
	}

	// ── Laminar required attributes ─────────────────────────────────────
	attrs := []attribute.KeyValue{
		// Laminar recognition: without this the span shows as a generic trace,
		// not as an LLM call in Laminar's timeline / cost analytics.
		attribute.String(attrLaminarSpanType, attrLaminarSpanLLM),

		// OpenTelemetry gen_ai semantic conventions (OTel Semantic Conventions
		// 1.27+). Laminar reads these for provider attribution and cost models.
		attribute.String("gen_ai.system", strings.ToLower(t.Provider)),
		attribute.String("gen_ai.request.model", t.Model),
		attribute.Int("gen_ai.usage.input_tokens", t.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", t.OutputTokens),
	}

	// ── Optional gen_ai attributes ──────────────────────────────────────
	if t.CachedTokens > 0 {
		// gen_ai.usage.cache_read_input_tokens is the semconv attribute for
		// Anthropic prompt-cache hits. Laminar uses this for cost correction.
		attrs = append(attrs, attribute.Int("gen_ai.usage.cache_read_input_tokens", t.CachedTokens))
	}
	if t.StopReason != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.finish_reasons", t.StopReason))
	}

	// ── Bodhyanga-specific context attributes (never PHI) ────────────────────
	if t.AgentID != "" {
		attrs = append(attrs, attribute.String("bodhyanga.agent_id", t.AgentID))
	}
	if t.CaseID != "" {
		// CaseID can be a real MRN — always hash before attaching to a span.
		// hashID() returns sha256[:8] (16 hex chars): correlatable but not
		// reversible.
		attrs = append(attrs, attribute.String("bodhyanga.case_id", hashID(t.CaseID)))
	}
	if t.TraceID != "" {
		attrs = append(attrs, attribute.String("bodhyanga.trace_id", t.TraceID))
	}
	if t.ToolCalls > 0 {
		attrs = append(attrs, attribute.Int("bodhyanga.tool_calls", t.ToolCalls))
	}

	_, span := e.tracer.Start(context.Background(),
		spanName,
		trace.WithTimestamp(start),
		trace.WithAttributes(attrs...),
	)
	if t.Error != "" {
		span.RecordError(fmt.Errorf("%s", t.Error))
	}
	span.End(trace.WithTimestamp(end))
}

// Shutdown flushes pending Laminar spans and tears down the tracer provider.
// Callers should pass a context with a sensible deadline (e.g. 5–10 seconds).
func (e *LaminarExporter) Shutdown(ctx context.Context) error {
	if e == nil || e.tp == nil {
		return nil
	}
	return e.tp.Shutdown(ctx)
}
