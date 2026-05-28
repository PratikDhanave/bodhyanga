package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/audio"
	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// AudioTranscriberAgent converts clinician-dictated audio into text on
// the bus. It subscribes to audio_transcription_request messages and
// emits audio_transcription_result messages (or an
// audio_transcription_failed message when the backend is unavailable).
//
// PHI policy:
//
//   - Audio bytes arrive base64-encoded in msg.Content (the HTTP handler
//     in cmd/care/audio.go does the upload-to-message conversion).
//   - Transcribed text is emitted in the outbound message Content and
//     in msg.Metadata under "text" so HTTP callers reading
//     /api/transcribe can splice it onto a medical.CaseState. It is
//     NEVER passed to env.Logf or to audit.Recorder.
//   - Audit events record: agent ID, success/failure, audio bytes
//     processed, audio duration (if reported by the backend), the
//     model_id, and the message routing triple. They NEVER record the
//     transcribed text or any clinical token derived from it.
//
// Fallback contract: when the injected Transcriber returns
// audio.ErrUnavailable (default build without the kronk tag, or a kronk
// runtime that failed to load), the agent emits a structured
// audio_transcription_failed message. The downstream caller is expected
// to fall back to typed clinical notes; the agent NEVER fabricates a
// transcription, which would be unsafe in a clinical context.
type AudioTranscriberAgent struct {
	// Transcriber is the audio backend. Injected at construction so
	// the agent test path uses the MockTranscriber and the production
	// path uses the kronk-backed implementation. Required.
	Transcriber audio.Transcriber

	// Audit is the optional audit.Recorder. When non-nil the agent
	// records one PHI-free KindMessage event per Transcribe call —
	// success and failure both — with the byte count, duration, model
	// id, and outcome. When nil the orchestrator's own KindMessage
	// hop is the only record (no per-call detail).
	Audit audit.Recorder
}

// audioRequest is the JSON shape of the audio_transcription_request
// message Content. The HTTP handler builds this from the multipart form
// upload; downstream agents that emit audio (e.g. a future RPM-listener
// for telephony) build the same envelope.
type audioRequest struct {
	// AudioBase64 is the raw audio bytes base64-encoded. We base64
	// rather than carrying raw bytes because protocol.Message.Content
	// is a string and the bus serialises it through JSON in several
	// code paths.
	AudioBase64 string `json:"audio_base64"`

	// MediaType is the MIME type of the encoded bytes ("audio/wav",
	// "audio/mpeg", "audio/flac"). Passed verbatim to the
	// Transcriber, which validates and returns ErrUnsupportedMedia
	// for unknown types.
	MediaType string `json:"media_type"`
}

// audioResult is the JSON shape emitted on the audio_transcription_result
// message. Carries the transcribed text plus a PHI-free meta block. The
// audit log does not see this struct — only the routing fields.
type audioResult struct {
	Text string                  `json:"text"`
	Meta audio.TranscriptionMeta `json:"meta"`
}

// audioFailure is the JSON shape emitted on the audio_transcription_failed
// message when the backend is unavailable or an upstream error occurred.
// The Reason is a short, PHI-free template string (never the raw error
// from the model SDK, which could in principle quote audio metadata).
type audioFailure struct {
	Reason    string `json:"reason"`
	Code      string `json:"code"`       // "unavailable" | "unsupported_media" | "invalid_request" | "internal"
	ModelID   string `json:"model_id,omitempty"`
	BytesIn   int    `json:"bytes_in,omitempty"`
}

// AgentID is the bus address used by other agents to send transcription
// requests. Hardcoded across the platform; changing it is a breaking
// change.
const AudioTranscriberAgentID = "audio_transcriber"

func (a *AudioTranscriberAgent) ID() string   { return AudioTranscriberAgentID }
func (a *AudioTranscriberAgent) Name() string { return "Audio Transcriber" }

// Capabilities are the strings other agents query the registry against.
// "transcribe_audio" is the primary capability; "dictation_intake"
// signals that this is the seam for clinician dictation entering the
// platform (vs. arbitrary audio classification).
func (a *AudioTranscriberAgent) Capabilities() []string {
	return []string{"transcribe_audio", "dictation_intake"}
}

// HandleMessage is the agent's only entry point. It decodes the audio
// request, calls the Transcriber, and emits exactly one message
// downstream:
//
//   - On success: audio_transcription_result addressed to the
//     msg.From (or msg.Metadata["reply_to"] if set) with the transcript
//     in Content and meta in Metadata.
//
//   - On failure: audio_transcription_failed with a structured Reason
//     and a Code field the caller branches on.
//
// HandleMessage NEVER returns a non-nil error — that would trigger
// audit.KindAgentError, which is misleading for the
// "backend-unavailable" case (which is the deterministic fallback path).
// We always emit a structured result/failure message instead.
func (a *AudioTranscriberAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	if a.Transcriber == nil {
		// No backend wired — emit a failure but do NOT return an
		// error. The empty-backend case is configuration, not a
		// runtime bug; surfacing it as KindAgentError would mask the
		// real signal (operator forgot the wiring).
		return a.failure(msg, "transcriber not configured", "internal", "", 0), nil
	}

	var req audioRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[audio_transcriber] invalid request envelope: %v", err)
		a.recordAudit(msg, "failure", 0, 0, "", "invalid_request")
		return a.failure(msg, "invalid audio request envelope", "invalid_request", "", 0), nil
	}

	audioBytes, err := base64.StdEncoding.DecodeString(req.AudioBase64)
	if err != nil {
		env.Logf("[audio_transcriber] base64 decode failed: %v", err)
		a.recordAudit(msg, "failure", 0, 0, "", "invalid_request")
		return a.failure(msg, "audio_base64 not decodable", "invalid_request", "", 0), nil
	}
	bytesIn := len(audioBytes)
	if bytesIn == 0 {
		env.Logf("[audio_transcriber] empty audio payload")
		a.recordAudit(msg, "failure", 0, 0, "", "invalid_request")
		return a.failure(msg, "empty audio payload", "invalid_request", "", 0), nil
	}

	// Call the backend. NOTE: we deliberately do NOT log the
	// transcribed text. env.Logf is wrapped by phi.LoggerWrapper in
	// production but the defence-in-depth rule is "don't log PHI at
	// the source, period".
	start := time.Now()
	text, meta, err := a.Transcriber.Transcribe(ctx, audioBytes, req.MediaType)
	wallClock := time.Since(start)

	if err != nil {
		code := classifyError(err)
		env.Logf("[audio_transcriber] backend error: code=%s bytes=%d wall=%dms",
			code, bytesIn, wallClock.Milliseconds())
		a.recordAudit(msg, "failure", bytesIn, meta.Duration, meta.ModelID, code)
		reason := "audio transcription backend unavailable; clinician should fall back to typed notes"
		if code == "unsupported_media" {
			reason = "audio media type not supported by the configured backend"
		}
		return a.failure(msg, reason, code, meta.ModelID, bytesIn), nil
	}

	// Success path. Log structured, PHI-free numerics only.
	env.Logf("[audio_transcriber] ok bytes=%d duration_ms=%d confidence=%.2f model=%s wall_ms=%d",
		bytesIn, meta.Duration.Milliseconds(), meta.Confidence, meta.ModelID, wallClock.Milliseconds())
	a.recordAudit(msg, "success", bytesIn, meta.Duration, meta.ModelID, "")

	return []agent.Message{a.result(msg, text, meta)}, nil
}

// result builds the success outbound message. The transcript travels in
// Content (PHI); meta travels in Metadata under audio_meta with PHI-free
// fields only. case_id / tenant_id / trace_id / patient_id are inherited
// from the request so the downstream consumer (test_planner /
// inbox_triage / cmd/care HTTP handler) can route on them.
func (a *AudioTranscriberAgent) result(req agent.Message, text string, meta audio.TranscriptionMeta) agent.Message {
	out := copyMeta(req.Metadata)
	out["text"] = text // PHI; only the HTTP response and downstream agent see this.
	out["audio_meta"] = map[string]any{
		"duration_ms": meta.Duration.Milliseconds(),
		"confidence":  meta.Confidence,
		"language":    meta.Language,
		"model_id":    meta.ModelID,
	}

	body, _ := json.Marshal(audioResult{Text: text, Meta: meta})
	to := replyTo(req)
	return agent.NewMessage(a.ID(), to, agent.RoleAgent, "audio_transcription_result", string(body), out)
}

// failure builds the deterministic-fallback outbound message. NEVER
// includes raw audio bytes, transcribed text, or the raw model error.
// reason is a short PHI-free template string; code is one of:
// unavailable, unsupported_media, invalid_request, internal.
func (a *AudioTranscriberAgent) failure(req agent.Message, reason, code, modelID string, bytesIn int) []agent.Message {
	out := copyMeta(req.Metadata)
	out["audio_unavailable"] = true
	out["audio_failure_code"] = code

	body, _ := json.Marshal(audioFailure{
		Reason:  reason,
		Code:    code,
		ModelID: modelID,
		BytesIn: bytesIn,
	})
	to := replyTo(req)
	return []agent.Message{
		agent.NewMessage(a.ID(), to, agent.RoleAgent, "audio_transcription_failed", string(body), out),
	}
}

// replyTo decides who receives the result. If the request set
// metadata["reply_to"], use it (HTTP handler does this so it can route
// back to a per-request listener). Otherwise echo to msg.From, which is
// the canonical agent-to-agent path.
func replyTo(req agent.Message) string {
	if v, ok := req.Metadata["reply_to"].(string); ok && v != "" {
		return v
	}
	return req.From
}

// recordAudit emits one PHI-free audit event for the call. The audit
// schema's allowlist (see pkg/audit/recorder.go FromMessage) already
// guarantees msg.Content / clinical text never reach the log; this helper
// is the explicit "agent ID + success/failure + bytes + duration"
// supplement the deliverables require.
//
// The four extra fields (outcome, bytes_in, duration_ms, model_id) are
// folded into msg.Metadata BEFORE calling audit.FromMessage so they ride
// in the standard correlation channel — but they are NOT picked up by
// pkg/audit.pickStrings (which whitelists only patient_id / case_id /
// trace_id / tenant_id). They land in env.Logf for operator visibility
// and on the Event.Type/From/To routing triple. If a deployment wants
// these fields persisted as first-class columns it should extend
// pkg/audit.Event in a separate PR — adding bytes_in / duration_ms is a
// PHI-conservative addition.
func (a *AudioTranscriberAgent) recordAudit(msg agent.Message, outcome string, bytesIn int, duration time.Duration, modelID, code string) {
	if a.Audit == nil {
		return
	}
	// Build the synthetic message envelope used to drive FromMessage.
	// We append outcome to the Type so the audit-log filter on Type
	// distinguishes success and failure ("audio_transcription_result"
	// vs "audio_transcription_failed.<code>") without needing
	// schema-level fields.
	syntheticType := "audio_transcription." + outcome
	if outcome == "failure" && code != "" {
		syntheticType += "." + code
	}
	envMsg := protocol.Message{
		ID:       msg.ID,
		From:     msg.From,
		To:       a.ID(),
		Type:     syntheticType,
		Metadata: msg.Metadata, // only correlation keys are read; see pickStrings.
	}
	ev := audit.FromMessage(a.ID(), envMsg)
	// Reason carries a PHI-FREE numeric summary. The kind stays
	// KindMessage (this is the agent's own per-call record); the
	// schema's Reason field is the only free-text slot available
	// without a recorder change.
	ev.Reason = fmt.Sprintf("bytes_in=%d duration_ms=%d model=%s", bytesIn, duration.Milliseconds(), modelID)
	_ = a.Audit.Record(ev)
}

// classifyError maps the backend's sentinel errors onto the audio failure
// codes the agent emits. We branch on errors.Is so kronk's wrapped errors
// still match the audio.ErrUnavailable / audio.ErrUnsupportedMedia
// predicates. Anything else is "internal" — the most conservative bucket;
// the agent's outbound message reason for "internal" is the same as
// "unavailable" (fall back to typed notes) so a mis-classification does
// not change clinician-facing behaviour.
func classifyError(err error) string {
	switch {
	case errors.Is(err, audio.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, audio.ErrUnsupportedMedia):
		return "unsupported_media"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "unavailable"
	default:
		// Defence in depth: if a future backend leaks the model
		// error string through, the message string itself never
		// reaches the audit log or the HTTP response — only this
		// classification.
		_ = strings.TrimSpace(err.Error())
		return "internal"
	}
}
