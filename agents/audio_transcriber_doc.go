// audio_transcriber.go — clinician-dictation audio → text agent.
//
// Overview:
//   Subscribes to audio_transcription_request messages. Decodes the
//   base64 audio payload, calls the injected pkg/audio.Transcriber
//   (kronk-backed on-prem Whisper in production, MockTranscriber in
//   tests and the default build), and emits either an
//   audio_transcription_result or audio_transcription_failed message.
//
// Key declarations:
//   - AudioTranscriberAgentID: the bus address ("audio_transcriber").
//   - AudioTranscriberAgent: implements pkg/agent.Agent.
//   - audioRequest / audioResult / audioFailure: wire shapes for the
//     three message types this agent uses.
//   - HandleMessage: never returns a non-nil error; surfaces backend
//     failure as a structured audio_transcription_failed message so
//     the caller falls back to typed notes (deterministic fallback).
//
// PHI policy:
//   - Audio bytes and transcribed text are PHI. They never reach
//     env.Logf or the audit log.
//   - Per-call audit events record only PHI-free numerics
//     (bytes_in, duration_ms, model_id, outcome).
//
// See doc.go for package-level documentation, agents/doc.go for the
// Bodh agent contract, and docs/agents.md for the agent reference.
package agents
