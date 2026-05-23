// runs_test.go — unit tests for the run manager and SSE event stream.
//
// Overview:
//   Verifies that newRunManager funnels broadcast-bus messages into the
//   correct runState based on metadata["case_id"], that case_state is
//   sanitized out of relayed events, and that runState.subscribe replays
//   history then fans out live events. Also covers startRunHandler's
//   fixture vs. ad-hoc presentation paths and runEventsHandler's SSE
//   framing end-to-end.
//
// Key declarations:
//   - TestRunManager_FunnelsMessagesMatchingCaseID
//   - TestRunManager_DropsLargeCaseStateFromMetadata
//   - TestRunState_SubscribeReceivesLiveAndReplaysHistory
//   - TestRunState_TerminalDiagnosisMarksClosed
//   - TestStartRunHandler_WithFixtureBootsThePipeline
//   - TestStartRunHandler_WithPresentationGeneratesCaseID
//   - TestStartRunHandler_RejectsNeitherFixtureNorPresentation
//   - TestRunEventsHandler_StreamsSSE
//
// See main_doc.go for command-level documentation.

package main
