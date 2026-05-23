// api_test.go — unit tests for the cmd/care HTTP API surface.
//
// Overview:
//   Covers the in-process LLM trace ring buffer, the JSON fixture loader,
//   and the /api/* handlers that read from the in-memory registries
//   (agents, panel, fine-tune, bench) plus the review-decision flow that
//   republishes payloads to the bus on approval.
//
// Key declarations:
//   - TestLLMTraceBuffer_SnapshotReturnsMostRecentFirst
//   - TestLLMTraceBuffer_RingBufferEvicts
//   - TestLoadFixtures_ReadsDemoJSONFromDir
//   - TestLoadFixtureByID_HitAndMiss
//   - TestAPIAgents_ReturnsRegisteredAgentsSortedByID
//   - TestAPIPanelReport_FlagsDiabetesWithoutA1c
//   - TestAPIFineTuneStatus_CountsByStatus
//   - TestAPIBenchUpload_ThenLatestReturnsRows
//   - TestAPIReviewDecide_PublishesResumePayloadOnApproval
//
// See main_doc.go for command-level documentation.

package main
