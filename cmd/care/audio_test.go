package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/agents"
	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/audio"
	"github.com/PratikDhanave/bodh/pkg/audit"
)

// silentEnv is a minimal agent.Environment for handler tests — drops
// logs, returns a fixed time. Mirrors agents/testhelpers_test.go.fakeEnv
// but lives in this package.
type silentEnv struct{}

func (silentEnv) Now() time.Time                        { return time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC) }
func (silentEnv) Logf(format string, args ...any)       {}

// buildMultipartAudio constructs a multipart/form-data body with the
// supplied audio bytes, media type, and form fields. Returns the body
// and the Content-Type header value (which carries the boundary).
func buildMultipartAudio(t *testing.T, audioBytes []byte, mediaType string, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="audio"; filename="dictation.wav"`)
	hdr.Set("Content-Type", mediaType)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("CreatePart: %v", err)
	}
	if _, err := part.Write(audioBytes); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("mw close: %v", err)
	}
	return &body, mw.FormDataContentType()
}

func TestTranscribeHandler_Success(t *testing.T) {
	mock := audio.NewMock()
	mock.Text = "follow up in two weeks"
	rec := audit.NewInMemory()
	a := &agents.AudioTranscriberAgent{Transcriber: mock, Audit: rec}

	body, ct := buildMultipartAudio(t, []byte("FAKE-WAV-BYTES"), "audio/wav",
		map[string]string{"case_id": "case-200", "patient_id": "pt-9"})
	r := httptest.NewRequest(http.MethodPost, "/api/transcribe", body)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()

	transcribeHandler(a, silentEnv{}, rec)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Text         string                  `json:"text"`
		Meta         audio.TranscriptionMeta `json:"meta"`
		AuditEventID string                  `json:"audit_event_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Text != "follow up in two weeks" {
		t.Errorf("Text = %q, want canned mock value", resp.Text)
	}
	if resp.Meta.ModelID != "mock" {
		t.Errorf("Meta.ModelID = %q, want mock", resp.Meta.ModelID)
	}
	if resp.AuditEventID == "" {
		t.Errorf("AuditEventID empty; want non-empty correlation handle")
	}
	if mock.CallCount != 1 {
		t.Errorf("mock called %d times, want 1", mock.CallCount)
	}

	// Confirm the recorded audit event carries the case/patient
	// correlation keys from the form fields.
	events := rec.List()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].CaseID != "case-200" || events[0].PatientID != "pt-9" {
		t.Errorf("audit case/patient = %q/%q, want case-200/pt-9",
			events[0].CaseID, events[0].PatientID)
	}
}

func TestTranscribeHandler_BackendUnavailableReturns503(t *testing.T) {
	mock := &audio.MockTranscriber{Err: audio.ErrUnavailable}
	a := &agents.AudioTranscriberAgent{Transcriber: mock, Audit: audit.NewInMemory()}

	body, ct := buildMultipartAudio(t, []byte("x"), "audio/wav", nil)
	r := httptest.NewRequest(http.MethodPost, "/api/transcribe", body)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()

	transcribeHandler(a, silentEnv{}, a.Audit)(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", w.Code, w.Body.String())
	}
	var resp transcribeFailureResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Code != "unavailable" {
		t.Errorf("Code = %q, want unavailable", resp.Code)
	}
	if !strings.Contains(resp.Error, "fall back to typed notes") {
		t.Errorf("Error missing fallback guidance: %q", resp.Error)
	}
}

func TestTranscribeHandler_UnsupportedMediaReturns415(t *testing.T) {
	mock := &audio.MockTranscriber{Err: audio.ErrUnsupportedMedia}
	a := &agents.AudioTranscriberAgent{Transcriber: mock}

	body, ct := buildMultipartAudio(t, []byte("x"), "video/mp4", nil)
	r := httptest.NewRequest(http.MethodPost, "/api/transcribe", body)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()

	transcribeHandler(a, silentEnv{}, nil)(w, r)

	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", w.Code)
	}
}

func TestTranscribeHandler_MissingAudioPartReturns400(t *testing.T) {
	a := &agents.AudioTranscriberAgent{Transcriber: audio.NewMock()}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("case_id", "c1")
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/transcribe", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()

	transcribeHandler(a, silentEnv{}, nil)(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestTranscribeHandler_WrongMethodReturns405(t *testing.T) {
	a := &agents.AudioTranscriberAgent{Transcriber: audio.NewMock()}
	r := httptest.NewRequest(http.MethodGet, "/api/transcribe", nil)
	w := httptest.NewRecorder()
	transcribeHandler(a, silentEnv{}, nil)(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// TestTranscribeHandler_NoAuditDoesNotPanic exercises the nil recorder
// path — operators who haven't wired audit at boot should still get a
// working transcribe endpoint.
func TestTranscribeHandler_NoAuditDoesNotPanic(t *testing.T) {
	a := &agents.AudioTranscriberAgent{Transcriber: audio.NewMock()}
	body, ct := buildMultipartAudio(t, []byte("x"), "audio/wav", nil)
	r := httptest.NewRequest(http.MethodPost, "/api/transcribe", body)
	r.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	transcribeHandler(a, silentEnv{}, nil)(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// Anchor the agent import so go vet doesn't drop it if test cases
// regress to silent helpers.
var _ agent.Message
