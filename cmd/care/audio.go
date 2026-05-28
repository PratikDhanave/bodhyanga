package main

// audio.go — POST /api/transcribe handler.
//
// Accepts multipart/form-data with an "audio" file part and an optional
// case_id / patient_id form value. Wraps the upload in an
// audio_transcription_request message, drives the AudioTranscriberAgent
// directly (rather than going through the bus's async fan-out — the HTTP
// caller wants a synchronous response with the transcript), and returns
// the result as JSON.
//
// Synchronous-by-design rationale: bus delivery is fire-and-forget
// (goroutine per Publish), so a multipart upload that waits on the bus
// would either need an SSE channel (the run-events shape) or per-request
// subscribe/unsubscribe gymnastics. For this endpoint, the audio agent's
// HandleMessage is a self-contained call that takes the audio in and
// returns the result in the same goroutine; we exercise it directly and
// still record the audit event through the agent's Audit field.
//
// PHI policy:
//   - The transcribed text is returned in the HTTP response body.
//     That is the only surface where PHI leaves this handler.
//   - Audio bytes never touch disk: they are read into a bounded
//     in-memory buffer, base64-encoded into the message, and discarded
//     when the handler returns.
//   - audit_event_id is the correlation handle to the PHI-free audit
//     event recorded by the agent (look up via GET /audit?kind=message
//     filtered by Type=audio_transcription.*).

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/audio"
	"github.com/PratikDhanave/bodh/pkg/audit"
)

// MaxAudioUploadBytes caps the multipart upload at 50 MiB. WAV @ 16kHz
// mono is ~1.9 MB/min, so 50 MiB ≈ 25 min of dictation — well above any
// realistic clinician note. Anything larger is rejected before the
// transcriber runs; the alternative (streaming chunked transcription) is
// a different endpoint.
const MaxAudioUploadBytes = 50 << 20 // 50 MiB

// transcribeResponse is the JSON returned by POST /api/transcribe on
// success. AuditEventID is the correlation key into /audit so the
// operator can pull the PHI-free audit row that pairs with this call.
type transcribeResponse struct {
	Text         string                  `json:"text"`
	Meta         audio.TranscriptionMeta `json:"meta"`
	AuditEventID string                  `json:"audit_event_id"`
}

// transcribeFailureResponse is the JSON returned when the backend is
// unavailable / unsupported / etc. The HTTP status is set per Code by
// the handler.
type transcribeFailureResponse struct {
	Error        string `json:"error"`
	Code         string `json:"code"`
	AuditEventID string `json:"audit_event_id,omitempty"`
}

// transcribeHandler returns the http.HandlerFunc for POST /api/transcribe.
// It is wired in main.go via apiServer.mount or as a standalone route.
//
// The agent is passed in (rather than constructed per request) so the
// same instance picks up the operator's audit.Recorder and audio backend
// at boot — and so tests can substitute a MockTranscriber.
func transcribeHandler(a *agents.AudioTranscriberAgent, env agent.Environment, auditRec audit.Recorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		// Hard-cap the request body BEFORE parsing the multipart
		// envelope. http.MaxBytesReader enforces the limit at the
		// io.Reader layer so a 5 GB upload doesn't first land in
		// memory before being rejected.
		r.Body = http.MaxBytesReader(w, r.Body, MaxAudioUploadBytes)

		if err := r.ParseMultipartForm(MaxAudioUploadBytes); err != nil {
			http.Error(w, "could not parse multipart: "+err.Error(), http.StatusBadRequest)
			return
		}

		file, hdr, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "missing 'audio' file part", http.StatusBadRequest)
			return
		}
		defer file.Close()

		audioBytes, err := readUploadedAudio(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mediaType := detectMediaType(hdr)

		// Pull optional correlation tokens from the form. These are
		// opaque routing IDs, NOT MRNs — the API contract is the
		// same as everywhere else in cmd/care.
		caseID := r.FormValue("case_id")
		patientID := r.FormValue("patient_id")
		if caseID == "" {
			caseID = "api-audio"
		}

		// Build the request envelope. The agent expects audio bytes
		// base64-encoded so the message Content is a valid JSON
		// string across the bus serialiser.
		req, _ := json.Marshal(struct {
			AudioBase64 string `json:"audio_base64"`
			MediaType   string `json:"media_type"`
		}{
			AudioBase64: base64.StdEncoding.EncodeToString(audioBytes),
			MediaType:   mediaType,
		})

		meta := map[string]any{
			"case_id":  caseID,
			"reply_to": "http_transcribe",
		}
		if patientID != "" {
			meta["patient_id"] = patientID
		}

		msg := agent.NewMessage("http_transcribe", agents.AudioTranscriberAgentID,
			agent.RoleUser, "audio_transcription_request", string(req), meta)

		// Drive the agent directly. HandleMessage never returns a
		// non-nil error by contract; backend failures arrive as
		// structured audio_transcription_failed messages.
		out, err := a.HandleMessage(r.Context(), msg, env)
		if err != nil {
			http.Error(w, "agent error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if len(out) != 1 {
			http.Error(w, fmt.Sprintf("agent emitted %d messages, want 1", len(out)),
				http.StatusInternalServerError)
			return
		}
		result := out[0]
		auditID := correlateAuditEvent(auditRec, msg.ID)

		if result.Type == "audio_transcription_failed" {
			var f struct {
				Reason string `json:"reason"`
				Code   string `json:"code"`
			}
			_ = json.Unmarshal([]byte(result.Content), &f)
			status := failureStatus(f.Code)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(transcribeFailureResponse{
				Error: f.Reason, Code: f.Code, AuditEventID: auditID,
			})
			return
		}

		var res struct {
			Text string                  `json:"text"`
			Meta audio.TranscriptionMeta `json:"meta"`
		}
		if err := json.Unmarshal([]byte(result.Content), &res); err != nil {
			http.Error(w, "agent response decode: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(transcribeResponse{
			Text: res.Text, Meta: res.Meta, AuditEventID: auditID,
		})
	}
}

// readUploadedAudio reads at most MaxAudioUploadBytes+1 from the upload;
// anything larger is rejected. We allow +1 so the io.LimitReader hitting
// EOF at exactly the cap is distinguishable from genuinely-larger uploads.
func readUploadedAudio(f multipart.File) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(f, MaxAudioUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read audio: %w", err)
	}
	if int64(len(buf)) > MaxAudioUploadBytes {
		return nil, errors.New("audio upload exceeds 50 MiB limit")
	}
	if len(buf) == 0 {
		return nil, errors.New("audio upload is empty")
	}
	return buf, nil
}

// detectMediaType prefers the explicit multipart Content-Type header on
// the file part. Falls back to application/octet-stream so the agent's
// own validation (audio.ErrUnsupportedMedia) provides the error path —
// rather than blindly guessing from the filename extension here.
func detectMediaType(hdr *multipart.FileHeader) string {
	if hdr == nil {
		return "application/octet-stream"
	}
	ct := hdr.Header.Get("Content-Type")
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

// failureStatus maps the agent's failure code to the appropriate HTTP
// status. The mapping is conservative — unavailable returns 503 (not
// 500) so monitoring distinguishes "backend off" from genuine bugs.
func failureStatus(code string) int {
	switch code {
	case "unsupported_media":
		return http.StatusUnsupportedMediaType
	case "invalid_request":
		return http.StatusBadRequest
	case "unavailable":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// correlateAuditEvent looks up the audit event the agent just recorded
// for this message ID and returns its UUID. Returns empty string when
// the recorder is nil or no matching event is present (the agent only
// records when its Audit field is set). The handler always succeeds
// even if this returns empty — the audit_event_id field is best-effort.
func correlateAuditEvent(rec audit.Recorder, messageID string) string {
	if rec == nil || messageID == "" {
		return ""
	}
	for _, e := range rec.List() {
		if e.MessageID == messageID && e.AgentID == agents.AudioTranscriberAgentID {
			return e.ID
		}
	}
	return ""
}
