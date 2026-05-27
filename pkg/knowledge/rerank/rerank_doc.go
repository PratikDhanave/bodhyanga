// Package rerank defines the pluggable cross-encoder reranker post-stage
// used by pkg/knowledge to refine BM25 + vector retrieval results.
//
// # Why a post-stage
//
// Hybrid retrieval (lexical BM25 + dense cosine, fused via RRF) is fast and
// cheap but blind to question structure. It surfaces documents that "mention
// diabetes" when the question is "diabetes + CKD + NSAID contraindication."
// A cross-encoder reranker reads (query, candidate) jointly and produces a
// relevance score that respects the full compositional intent. For medical
// RAG the difference matters — see docs/ai-features.md § RAG reranker.
//
// # Default behaviour
//
// The default is no reranker: knowledge.Store.Search returns the BM25+vector
// fused order unchanged. The reranker is opt-in via the WithReranker
// constructor options on the in-memory and hybrid stores.
//
// # Rule-based fallback
//
// AGENTS.md invariant #3 — every LLM-backed agent has a deterministic
// fallback, and the fallback is the contract — applies here. If the reranker
// returns an error (model not loaded, timeout, transport failure), the
// knowledge store logs the failure via the supplied logger and returns the
// original retrieval order. The calling agent never observes a "rerank
// failed" error; it sees the same shape of result it would have seen with
// the reranker disabled.
//
// # Implementations
//
//   - Passthrough — returns the input order with RerankScore = Score.
//     The default when no reranker is configured.
//   - kronk.Reranker (under //go:build kronk) — wraps the ardanlabs/kronk
//     cross-encoder SDK against gpustack/bge-reranker-v2-m3-Q8_0. The
//     default build supplies a stub that returns a clear "rebuild with
//     -tags=kronk" error so cmd/care falls back to the pass-through.
//
// # Audit + PHI
//
// Per the audit-surface invariant, callers must NOT log the query string or
// the candidate text via observability.Logger directly — both contain
// patient-derived content. Wrap the logger with phi.LoggerWrapper before
// passing it to a Reranker. Reranker scores and timings are PHI-free and
// safe to record.
package rerank
