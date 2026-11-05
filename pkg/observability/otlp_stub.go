// otlp_stub.go — no-op shim used when the `otel` build tag is NOT set.
//
// Bodhyanga's default build does not link the OTel SDK. This stub keeps the
// OTLPLLMExporter type and methods compiling so callers can reference them
// unconditionally if they wish, while the real implementation lives in
// otlp.go behind `//go:build otel`.
//
// To get real OTLP export: build with `go build -tags otel ./cmd/care`.

//go:build !otel

package observability

import (
	"context"
	"errors"
)

// OTLPLLMExporter is a no-op stub in the default build. Methods are safe to
// call but do nothing; Record never produces a span.
type OTLPLLMExporter struct{}

// NewOTLPLLMExporter returns an error in the default build to signal the
// SDK was not linked. Build with `-tags otel` for the real exporter.
func NewOTLPLLMExporter(_ context.Context, _ string) (*OTLPLLMExporter, error) {
	return nil, errors.New("observability: OTLP exporter requires the 'otel' build tag")
}

// Record is a no-op when the OTel SDK is not linked.
func (e *OTLPLLMExporter) Record(_ LLMTrace) {}

// Shutdown is a no-op when the OTel SDK is not linked.
func (e *OTLPLLMExporter) Shutdown(_ context.Context) error { return nil }
