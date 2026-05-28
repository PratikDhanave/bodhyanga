package audio

import (
	"context"
	"errors"
	"time"
)

// Transcriber is the contract every audio backend (kronk, mock, future
// provider) implements. The interface is deliberately small so the agent in
// agents/audio_transcriber.go can call any implementation through the same
// seam without compiling-in the model runtime.
//
// PHI policy:
//
//   - audio bytes are PHI; implementations process them in-process and
//     must not persist or forward them.
//   - the returned text IS PHI; callers must NOT log it or attach it to
//     audit events. It travels in protocol.Message.Content and in the
//     HTTP response body — that is the entire allowed surface.
//   - the returned TranscriptionMeta is PHI-free by contract — see its
//     field doc.
//
// Cancellation: implementations must honour ctx (the HTTP handler attaches
// a request-scoped deadline). A canceled call returns ctx.Err() and any
// partial transcription is discarded.
type Transcriber interface {
	// Transcribe converts audio bytes into text. mediaType is the
	// MIME type (e.g. "audio/wav", "audio/mpeg"); implementations may
	// accept a subset and should return ErrUnsupportedMedia for the
	// rest. The returned text is the clinician's dictated note; the
	// returned meta is PHI-free and audit-safe.
	Transcribe(ctx context.Context, audio []byte, mediaType string) (text string, meta TranscriptionMeta, err error)
}

// TranscriptionMeta is the side-channel that travels alongside the text.
// Every field here is safe to record in the audit log and on OTel spans.
//
// Anything that COULD encode clinical content (e.g. word-level timestamps,
// speaker labels, alternate hypotheses) belongs in the text payload, not
// here. The PHI guarantee of the audit log depends on this rule.
type TranscriptionMeta struct {
	// Duration is the audio clip length (not the wall-clock processing
	// time). Zero when the implementation cannot determine it.
	Duration time.Duration `json:"duration,omitempty"`

	// Confidence is the model's posterior on the whole transcript in
	// the 0.0–1.0 range. Zero when not reported. Used by the agent to
	// decide whether to flag low-confidence output to the clinician.
	Confidence float64 `json:"confidence,omitempty"`

	// Language is the BCP-47 tag the model detected or was constrained
	// to (e.g. "en", "en-US"). Empty when not reported.
	Language string `json:"language,omitempty"`

	// ModelID identifies the model that produced the transcript (e.g.
	// "mradermacher/Qwen2-Audio-7B.Q8_0", "mock"). Recorded in the
	// audit log for forensic reconstruction — which model produced
	// which call.
	ModelID string `json:"model_id,omitempty"`
}

// ErrUnsupportedMedia is the sentinel returned by Transcribe when the
// mediaType is one the implementation can't decode. Callers (the agent +
// HTTP handler) map it to a 415 response.
var ErrUnsupportedMedia = errors.New("audio: unsupported media type")

// ErrUnavailable is the sentinel returned by stubbed-out builds (no kronk
// tag) and by implementations that detected a missing runtime dependency
// at call time. The agent's rule-based fallback maps it to a structured
// "audio path unavailable" message so the test_planner / inbox_triage
// caller can fall back to typed clinical notes.
var ErrUnavailable = errors.New("audio: transcription backend unavailable")
