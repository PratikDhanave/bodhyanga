// audio_transcriber_test.go — table-driven tests for the audio
// transcriber agent.
//
// Covers:
//   - happy path: MockTranscriber returns text, agent emits
//     audio_transcription_result with the text in Content + Metadata.
//   - audit invariant: transcribed text NEVER leaks into the audit
//     event Reason / Type / any other field.
//   - rule-based fallback: audio.ErrUnavailable yields an
//     audio_transcription_failed message (NOT a non-nil HandleMessage
//     error) so the orchestrator routes it normally and the clinician
//     workflow falls back to typed notes.
//   - unsupported media, invalid base64, empty audio, nil-Transcriber:
//     all surface as structured failures with distinct Code values.
//   - agent ID / Name / Capabilities sanity check.
//
// See doc.go for package-level documentation.
package agents
