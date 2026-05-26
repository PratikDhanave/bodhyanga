//go:build !kronk

// kronk_factory_stub.go — default build stub for the kronk Factory.
//
// The real implementation lives in kronk_factory.go behind //go:build kronk.
// This file keeps the default build (and CI) free of any kronk dependency.
// Callers that try to construct the kronk factory without the build tag
// get a clear error explaining how to enable it.
//
// Note: KronkOptions / ErrKronkNotBuilt live in kronk_common.go because they
// are part of the API surface under both build tags — only the Factory
// constructor implementation differs.

package pool

import (
	"context"
	"io"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// NewKronkFactory returns a Factory that always errors with ErrKronkNotBuilt
// in the default build. Callers can still register the route in cmd/care
// without a compile-time dependency on the kronk SDK.
func NewKronkFactory(_ KronkOptions) Factory {
	return func(_ context.Context, _ ModelSpec) (llm.Client, io.Closer, error) {
		return nil, nil, ErrKronkNotBuilt
	}
}
