// laminar_stub.go — no-op shim used when the `otel` build tag is NOT set.
//
// Keeps LaminarExporter and its methods compiling so callers can reference
// them unconditionally if they wish, while the real implementation lives in
// laminar.go behind `//go:build otel`.
//
// To get real Laminar export: build with `go build -tags otel ./cmd/care`.

//go:build !otel

package observability

import (
	"context"
	"errors"
)

// LaminarExporter is a no-op stub in the default build. Methods are safe to
// call but do nothing; Record never produces a span.
type LaminarExporter struct{}

// NewLaminarExporter returns an error in the default build to signal the
// OTel SDK was not linked. Build with `-tags otel` for the real exporter.
func NewLaminarExporter(_ context.Context) (*LaminarExporter, error) {
	return nil, errors.New("observability: Laminar exporter requires the 'otel' build tag")
}

// Record is a no-op when the OTel SDK is not linked.
func (e *LaminarExporter) Record(_ LLMTrace) {}

// Shutdown is a no-op when the OTel SDK is not linked.
func (e *LaminarExporter) Shutdown(_ context.Context) error { return nil }
