// Package audio is Bodh's seam for on-premise audio transcription of
// clinician-dictated notes. It defines a small Transcriber interface and
// a PHI-free TranscriptionMeta side-channel. Implementations live behind
// build tags so the default Bodh build links no audio runtime:
//
//   - pkg/audio/mock.go        — MockTranscriber (default, no deps)
//   - pkg/audio/kronk/         — Ardan Labs kronk SDK (//go:build kronk)
//
// Wiring:
//
//   - cmd/care/audio.go publishes audio_transcription_request messages.
//   - agents/audio_transcriber.go subscribes, calls the injected
//     Transcriber, emits audio_transcription_result, and records a
//     PHI-free audit.KindMessage event.
//
// HIPAA win
//
//	The kronk backend runs the Whisper/Qwen2-Audio model in-process on
//	the deployment host. No PHI leaves the box — neither the audio bytes
//	nor the transcribed text. That is the property that distinguishes
//	this seam from a hosted Whisper API and the reason this stack exists.
//
// Failure mode
//
//	ErrUnavailable is the deterministic fallback signal. When the audio
//	backend is missing or broken, the agent emits a structured error
//	message so the clinician's workflow falls back to typed notes — NOT
//	to a fabricated transcription. Fabricating clinical text would be
//	unsafe by definition.
//
// See doc.go is the package overview; types and the Transcriber contract
// live in audio.go.
package audio
