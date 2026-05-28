// mock.go — MockTranscriber, the no-runtime audio backend used by tests
// and by the default cmd/care build (when -tags=kronk is not set).
//
// Key declarations:
//   - MockTranscriber: implements pkg/audio.Transcriber.
//   - NewMock: returns a MockTranscriber with sane canned defaults
//     (non-clinical text, model_id="mock").
//   - Fields Text / Meta / Err / CallCount are exported so tests can
//     inject canned values or errors.
//
// Failure-mode note: MockTranscriber is NOT the deterministic fallback
// for the agent. The fallback path is "return audio.ErrUnavailable so
// the agent emits audio_transcription_failed and the clinician falls
// back to typed notes". MockTranscriber exists only so the agent and
// HTTP handler are exercisable end-to-end without a model runtime.
//
// See doc.go for package-level documentation.
package audio
