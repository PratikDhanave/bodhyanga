//go:build kronk

// Package kronk wraps the ardanlabs/kronk cross-encoder SDK to satisfy the
// rerank.Reranker interface.
//
// This file is only compiled with `-tags=kronk`. The default Bodhyanga build uses
// the sibling stub (kronk_rerank_stub.go) so the project's go.sum stays
// minimal and the binary does not transitively pull in any kronk deps.
//
// To enable:
//
//	go get github.com/ardanlabs/kronk
//	go build -tags=kronk ./cmd/care
//	BODHYANGA_RERANKER=kronk ./care
//
// Reference: https://github.com/ardanlabs/kronk/blob/main/examples/rerank/main.go
package kronk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/PratikDhanave/bodhyanga/pkg/knowledge/rerank"

	// The kronk SDK; only present in the build graph under the kronk tag.
	// The exact import path follows the ardanlabs/kronk example.
	kronksdk "github.com/ardanlabs/kronk/foundation/client"
)

// defaultModelSource is the cross-encoder used by the ardanlabs/kronk rerank
// example. bge-reranker-v2-m3 in Q8_0 quantization fits comfortably in
// commodity GPUs and produces well-calibrated relevance scores.
const defaultModelSource = "gpustack/bge-reranker-v2-m3-Q8_0"

// Reranker is the kronk-backed cross-encoder. Construct it once at startup
// and share the instance across agents — the underlying HTTP client to the
// kronk server is safe for concurrent use.
type Reranker struct {
	client      *kronksdk.Client
	modelSource string
}

// Option configures a Reranker.
type Option func(*Reranker)

// WithModelSource overrides the default bge-reranker-v2-m3-Q8_0 model.
func WithModelSource(src string) Option {
	return func(r *Reranker) {
		if src != "" {
			r.modelSource = src
		}
	}
}

// New constructs a kronk-backed Reranker. The client must already be
// configured against the kronk server endpoint; this wrapper only adds the
// model-source default and the Candidate → kronk → Scored translation.
//
// The first argument is typed `any` so the call signature is identical
// between this real build and the default-build stub — callers in
// cmd/care can pass nil and let New() build a default client from
// BODHYANGA_KRONK_ENDPOINT.
func New(client any, opts ...Option) (*Reranker, error) {
	c, ok := client.(*kronksdk.Client)
	if !ok || c == nil {
		// Default: build a client from BODHYANGA_KRONK_ENDPOINT.
		endpoint := os.Getenv("BODHYANGA_KRONK_ENDPOINT")
		if endpoint == "" {
			return nil, errors.New("rerank/kronk: nil client and BODHYANGA_KRONK_ENDPOINT not set")
		}
		c = kronksdk.New(endpoint)
	}
	r := &Reranker{
		client:      c,
		modelSource: defaultModelSource,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

// Rerank implements rerank.Reranker. It batches every candidate's Text into a
// single kronk Rerank call against r.modelSource, then sorts the response by
// the model's relevance score (descending). Ties keep input order.
//
// Failure modes — all returned as a non-nil error so the caller (the
// knowledge store) can degrade to the unranked input order:
//
//   - context deadline / cancellation
//   - kronk transport / RPC error
//   - empty / malformed response from the model
//   - score-index mismatch (response cardinality != input cardinality)
func (r *Reranker) Rerank(ctx context.Context, query string, candidates []rerank.Candidate) ([]rerank.Scored, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	docs := make([]string, len(candidates))
	for i, c := range candidates {
		docs[i] = c.Text
	}

	// Mirrors examples/rerank/main.go in ardanlabs/kronk: a single Rerank
	// call against the configured model source.
	resp, err := r.client.Rerank(ctx, kronksdk.RerankInput{
		Model:     r.modelSource,
		Query:     query,
		Documents: docs,
	})
	if err != nil {
		return nil, fmt.Errorf("rerank/kronk: rerank call: %w", err)
	}
	if len(resp.Results) != len(candidates) {
		return nil, fmt.Errorf("rerank/kronk: model returned %d results for %d candidates", len(resp.Results), len(candidates))
	}

	out := make([]rerank.Scored, len(candidates))
	for _, res := range resp.Results {
		if res.Index < 0 || res.Index >= len(candidates) {
			return nil, fmt.Errorf("rerank/kronk: out-of-range index %d (len %d)", res.Index, len(candidates))
		}
		out[res.Index] = rerank.Scored{
			Candidate:   candidates[res.Index],
			RerankScore: res.RelevanceScore,
		}
	}
	// Sort descending by RerankScore; stable so equal scores preserve input order.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].RerankScore > out[j].RerankScore
	})
	return out, nil
}
