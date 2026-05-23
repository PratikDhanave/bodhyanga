// testhelpers_knowledge_test.go — fake knowledge.Store for agent unit tests.
//
// Overview:
//   Provides fakeKnowledge: a stub implementation of knowledge.Store whose
//   Search method records every (query, k) pair and returns canned citations
//   or a canned error. Used by diagnostician RAG tests to assert that the
//   correct retrieval query is built and the resulting citations flow onto
//   the DiagnosisProposal.
//
// Key declarations:
//   - fakeKnowledge: in-memory stub Store
//   - fakeKnowledgeCall: per-call record (query + k)
//   - errSearch: shared error value for negative-path tests
//
// See doc.go for package-level documentation.

package agents
