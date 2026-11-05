package agents

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/audio"
	"github.com/PratikDhanave/bodhyanga/pkg/audit"
)

// buildRequest builds an audio_transcription_request message with the
// supplied raw bytes and media type. case_id / patient_id are stamped so
// the audit recorder picks them up.
func buildRequest(raw []byte, mediaType string) agent.Message {
	body, _ := json.Marshal(audioRequest{
		AudioBase64: base64.StdEncoding.EncodeToString(raw),
		MediaType:   mediaType,
	})
	return agent.NewMessage(
		"cmd_care",
		AudioTranscriberAgentID,
		agent.RoleUser,
		"audio_transcription_request",
		string(body),
		map[string]any{
			"case_id":    "case-001",
			"patient_id": "pt-001",
			"reply_to":   "cmd_care",
		},
	)
}

// TestAudioTranscriber_SuccessEmitsResult drives the happy path with a
// MockTranscriber, asserts the outbound message shape, asserts the
// transcript travels in Content + meta.text but does NOT leak into the
// audit event reason.
func TestAudioTranscriber_SuccessEmitsResult(t *testing.T) {
	mock := audio.NewMock()
	mock.Text = "patient reports chest pain"
	rec := audit.NewInMemory()
	a := &AudioTranscriberAgent{Transcriber: mock, Audit: rec}

	msg := buildRequest([]byte("fake-audio-bytes"), "audio/wav")
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err = %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 outbound msg, got %d", len(out))
	}
	res := out[0]
	if res.Type != "audio_transcription_result" {
		t.Errorf("Type = %q, want audio_transcription_result", res.Type)
	}
	if res.To != "cmd_care" {
		t.Errorf("To = %q, want cmd_care", res.To)
	}
	var body audioResult
	if err := json.Unmarshal([]byte(res.Content), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Text != "patient reports chest pain" {
		t.Errorf("Content.Text = %q, want canned mock value", body.Text)
	}
	if body.Meta.ModelID != "mock" {
		t.Errorf("Meta.ModelID = %q, want mock", body.Meta.ModelID)
	}
	if textMeta, ok := res.Metadata["text"].(string); !ok || textMeta != body.Text {
		t.Errorf("Metadata[text] missing or wrong, got %v", res.Metadata["text"])
	}
	if _, ok := res.Metadata["audio_meta"]; !ok {
		t.Errorf("Metadata[audio_meta] missing")
	}
	if mock.CallCount != 1 {
		t.Errorf("mock called %d times, want 1", mock.CallCount)
	}

	// Audit invariant: the transcribed text must NOT appear in any
	// audit event field.
	events := rec.List()
	if len(events) != 1 {
		t.Fatalf("want 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if ev.Kind != audit.KindMessage {
		t.Errorf("Kind = %q, want %q", ev.Kind, audit.KindMessage)
	}
	if !strings.Contains(ev.Type, "audio_transcription.success") {
		t.Errorf("Type = %q, want audio_transcription.success", ev.Type)
	}
	if ev.AgentID != AudioTranscriberAgentID {
		t.Errorf("AgentID = %q, want %q", ev.AgentID, AudioTranscriberAgentID)
	}
	if ev.CaseID != "case-001" || ev.PatientID != "pt-001" {
		t.Errorf("CaseID/PatientID = %q/%q, want case-001/pt-001", ev.CaseID, ev.PatientID)
	}
	forbidden := []string{"chest pain", "patient reports"}
	for _, frag := range forbidden {
		if strings.Contains(ev.Reason, frag) {
			t.Errorf("audit Reason leaked transcribed text %q: %q", frag, ev.Reason)
		}
	}
	if !strings.Contains(ev.Reason, "bytes_in=") || !strings.Contains(ev.Reason, "model=mock") {
		t.Errorf("audit Reason missing numeric summary: %q", ev.Reason)
	}
}

// TestAudioTranscriber_BackendUnavailableEmitsFailure is the rule-based
// fallback path: when the transcriber returns ErrUnavailable, the agent
// must emit an audio_transcription_failed message (not fabricate a
// transcription, not return an error).
func TestAudioTranscriber_BackendUnavailableEmitsFailure(t *testing.T) {
	mock := &audio.MockTranscriber{Err: audio.ErrUnavailable}
	rec := audit.NewInMemory()
	a := &AudioTranscriberAgent{Transcriber: mock, Audit: rec}

	out, err := a.HandleMessage(context.Background(), buildRequest([]byte("x"), "audio/wav"), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err = %v; agent must NEVER return error for backend-unavailable", err)
	}
	if len(out) != 1 || out[0].Type != "audio_transcription_failed" {
		t.Fatalf("want one audio_transcription_failed msg, got %#v", out)
	}
	var body audioFailure
	if err := json.Unmarshal([]byte(out[0].Content), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Code != "unavailable" {
		t.Errorf("Code = %q, want unavailable", body.Code)
	}
	if !strings.Contains(body.Reason, "fall back to typed notes") {
		t.Errorf("Reason missing fallback guidance: %q", body.Reason)
	}
	if out[0].Metadata["audio_unavailable"] != true {
		t.Errorf("Metadata[audio_unavailable] = %v, want true", out[0].Metadata["audio_unavailable"])
	}

	// Audit event must record the failure outcome but no PHI.
	events := rec.List()
	if len(events) != 1 {
		t.Fatalf("want 1 audit event, got %d", len(events))
	}
	if !strings.Contains(events[0].Type, "failure.unavailable") {
		t.Errorf("audit Type = %q, want failure.unavailable", events[0].Type)
	}
}

// TestAudioTranscriber_UnsupportedMedia tests the 415-style path.
func TestAudioTranscriber_UnsupportedMedia(t *testing.T) {
	mock := &audio.MockTranscriber{Err: audio.ErrUnsupportedMedia}
	a := &AudioTranscriberAgent{Transcriber: mock}

	out, err := a.HandleMessage(context.Background(), buildRequest([]byte("x"), "video/mp4"), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err = %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 msg, got %d", len(out))
	}
	var body audioFailure
	if err := json.Unmarshal([]byte(out[0].Content), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Code != "unsupported_media" {
		t.Errorf("Code = %q, want unsupported_media", body.Code)
	}
}

// TestAudioTranscriber_InvalidBase64 exercises the malformed-envelope path.
func TestAudioTranscriber_InvalidBase64(t *testing.T) {
	a := &AudioTranscriberAgent{Transcriber: audio.NewMock()}
	body, _ := json.Marshal(audioRequest{AudioBase64: "%%%not base64%%%", MediaType: "audio/wav"})
	msg := agent.NewMessage("cmd_care", AudioTranscriberAgentID, agent.RoleUser,
		"audio_transcription_request", string(body), map[string]any{"case_id": "c"})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err = %v", err)
	}
	if len(out) != 1 || out[0].Type != "audio_transcription_failed" {
		t.Fatalf("want failure, got %#v", out)
	}
	var f audioFailure
	if err := json.Unmarshal([]byte(out[0].Content), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Code != "invalid_request" {
		t.Errorf("Code = %q, want invalid_request", f.Code)
	}
}

// TestAudioTranscriber_EmptyAudio rejects the zero-bytes case before
// calling the backend.
func TestAudioTranscriber_EmptyAudio(t *testing.T) {
	mock := audio.NewMock()
	a := &AudioTranscriberAgent{Transcriber: mock}
	out, err := a.HandleMessage(context.Background(), buildRequest(nil, "audio/wav"), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err = %v", err)
	}
	if len(out) != 1 || out[0].Type != "audio_transcription_failed" {
		t.Fatalf("want failure, got %#v", out)
	}
	if mock.CallCount != 0 {
		t.Errorf("backend called %d times for empty payload, want 0", mock.CallCount)
	}
}

// TestAudioTranscriber_NilTranscriberSurfacesFailure ensures the
// agent stays non-panicking when an operator forgets to wire the
// backend.
func TestAudioTranscriber_NilTranscriberSurfacesFailure(t *testing.T) {
	a := &AudioTranscriberAgent{} // Transcriber intentionally nil
	out, err := a.HandleMessage(context.Background(), buildRequest([]byte("x"), "audio/wav"), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err = %v", err)
	}
	if len(out) != 1 || out[0].Type != "audio_transcription_failed" {
		t.Fatalf("want failure, got %#v", out)
	}
	var f audioFailure
	_ = json.Unmarshal([]byte(out[0].Content), &f)
	if f.Code != "internal" {
		t.Errorf("Code = %q, want internal", f.Code)
	}
}

// TestAudioTranscriber_AgentMetadata sanity-checks ID / Name / Capabilities.
func TestAudioTranscriber_AgentMetadata(t *testing.T) {
	a := &AudioTranscriberAgent{Transcriber: audio.NewMock()}
	if a.ID() != AudioTranscriberAgentID {
		t.Errorf("ID = %q, want %q", a.ID(), AudioTranscriberAgentID)
	}
	if a.Name() == "" {
		t.Errorf("Name = empty")
	}
	caps := a.Capabilities()
	wantCap := "transcribe_audio"
	found := false
	for _, c := range caps {
		if c == wantCap {
			found = true
		}
	}
	if !found {
		t.Errorf("Capabilities = %v, missing %q", caps, wantCap)
	}
}
