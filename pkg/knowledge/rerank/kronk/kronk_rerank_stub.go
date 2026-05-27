//go:build !kronk

// Package kronk contains the build-tagged Kronk-backed reranker. This file
// is the default-build stub: it satisfies the public API surface (so callers
// can import the package unconditionally) but every constructor returns an
// error directing the operator to rebuild with `-tags=kronk`.
//
// This keeps the default build graph free of kronk transitive dependencies —
// per AGENTS.md "Don't add an OTel-tagged dep to the default build" applied
// to the kronk dep family.
package kronk

import (
	"context"
	"errors"

	"github.com/PratikDhanave/bodh/pkg/knowledge/rerank"
)

// ErrNotBuilt is returned by every constructor in the default build.
var ErrNotBuilt = errors.New("rerank/kronk: kronk reranker is build-tag isolated; rebuild with -tags=kronk")

// Reranker is the stub. Its Rerank method always returns ErrNotBuilt so the
// caller (knowledge store) falls back to the original retrieval order.
type Reranker struct{}

// Option exists in the stub purely so caller code can compile against the
// real and stub builds with the same import surface.
type Option func(*Reranker)

// WithModelSource is a no-op in the default build.
func WithModelSource(string) Option { return func(*Reranker) {} }

// New returns ErrNotBuilt. cmd/care logs the error and falls back to the
// pass-through reranker. The single-argument signature matches the real
// build's *kronksdk.Client position with `any` so the call site is
// build-tag agnostic — pass nil when calling from the default build.
func New(_ any, _ ...Option) (*Reranker, error) {
	return nil, ErrNotBuilt
}

// Rerank implements rerank.Reranker for compile compatibility. Always
// returns ErrNotBuilt; the knowledge store treats that as "reranker
// unavailable" and returns the input order unchanged.
func (*Reranker) Rerank(_ context.Context, _ string, _ []rerank.Candidate) ([]rerank.Scored, error) {
	return nil, ErrNotBuilt
}
