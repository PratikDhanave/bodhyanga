// differential_ranker.go — LLM helper that re-ranks Differential lists.
//
// Overview:
//   DifferentialRanker takes a DiagnosisProposal produced by any
//   diagnostician and asks an LLM to re-order its Differential list
//   using the rest of the CaseState as a tiebreaker. The primary
//   Diagnosis is never modified — this is a small compositional pass
//   that callers can plug in after the main agent without changing the
//   diagnostician implementation.
//
//   The ranker degrades gracefully: any LLM transport error, malformed
//   JSON, or empty array returns the original differential with a nil
//   error, so the surrounding pipeline keeps running.
//
// Key declarations:
//   - DifferentialRanker: struct carrying Client/Timeout/MaxTokens.
//   - Rerank(ctx, state, proposal): returns the new ordering.
//   - parseRankedList: accepts a bare array or a single-key wrapper.
//   - differentialRankerSystemPrompt: cached prompt; cache eligible.
//   - differentialRankerSchema: array-of-strings, maxItems=10.
//
// See doc.go for package-level documentation.

package agents
