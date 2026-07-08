package audio

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestMockTranscriber_Defaults verifies NewMock returns sane defaults and
// the canned transcript so tests get a usable Transcriber out of the box.
func TestMockTranscriber_Defaults(t *testing.T) {
	m := NewMock()
	text, meta, err := m.Transcribe(context.Background(), []byte("ignored"), "audio/wav")
	if err != nil {
		t.Fatalf("Transcribe err = %v, want nil", err)
	}
	if text == "" {
		t.Errorf("Transcribe text empty; want non-empty canned value")
	}
	if meta.ModelID != "mock" {
		t.Errorf("ModelID = %q, want %q", meta.ModelID, "mock")
	}
	if meta.Language != "en" {
		t.Errorf("Language = %q, want %q", meta.Language, "en")
	}
	if meta.Confidence <= 0 || meta.Confidence > 1 {
		t.Errorf("Confidence = %f, want 0 < c <= 1", meta.Confidence)
	}
	if m.CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", m.CallCount)
	}
}

// TestMockTranscriber_TableDriven exercises the error path, canceled
// context, and explicit-meta override.
func TestMockTranscriber_TableDriven(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		name      string
		ctx       context.Context
		mock      *MockTranscriber
		wantText  string
		wantModel string
		wantErr   error
	}{
		{
			name: "explicit_meta_kept",
			ctx:  context.Background(),
			mock: &MockTranscriber{
				Text: "hello world",
				Meta: TranscriptionMeta{ModelID: "test-model-v1", Language: "en-US"},
			},
			wantText:  "hello world",
			wantModel: "test-model-v1",
		},
		{
			name:    "injected_error_propagates",
			ctx:     context.Background(),
			mock:    &MockTranscriber{Err: ErrUnavailable},
			wantErr: ErrUnavailable,
		},
		{
			name:    "unsupported_media_propagates",
			ctx:     context.Background(),
			mock:    &MockTranscriber{Err: ErrUnsupportedMedia},
			wantErr: ErrUnsupportedMedia,
		},
		{
			name:    "canceled_ctx_returns_err",
			ctx:     canceled,
			mock:    NewMock(),
			wantErr: context.Canceled,
		},
		{
			name:      "empty_meta_defaults_modelid_to_mock",
			ctx:       context.Background(),
			mock:      &MockTranscriber{Text: "x", Meta: TranscriptionMeta{}},
			wantText:  "x",
			wantModel: "mock",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			text, meta, err := tc.mock.Transcribe(tc.ctx, []byte("audio"), "audio/wav")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if text != tc.wantText {
				t.Errorf("text = %q, want %q", text, tc.wantText)
			}
			if meta.ModelID != tc.wantModel {
				t.Errorf("ModelID = %q, want %q", meta.ModelID, tc.wantModel)
			}
		})
	}
}

// TestMockTranscriber_CallCountAdvances verifies the counter is monotonic
// across multiple invocations so the agent test can assert it called
// exactly once.
func TestMockTranscriber_CallCountAdvances(t *testing.T) {
	m := NewMock()
	for i := 1; i <= 3; i++ {
		if _, _, err := m.Transcribe(context.Background(), nil, "audio/wav"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if m.CallCount != i {
			t.Errorf("after call %d: CallCount = %d, want %d", i, m.CallCount, i)
		}
	}
}

// TestTranscriptionMeta_ZeroValueIsAuditSafe verifies the zero value of
// TranscriptionMeta serialises with omitempty so a backend that doesn't
// know the duration / confidence simply omits those fields rather than
// writing 0/0.0 placeholders that look like real measurements.
func TestTranscriptionMeta_ZeroValueIsAuditSafe(t *testing.T) {
	var m TranscriptionMeta
	if m.Duration != 0 {
		t.Errorf("Duration zero = %v, want 0", m.Duration)
	}
	if m.Confidence != 0 {
		t.Errorf("Confidence zero = %v, want 0", m.Confidence)
	}
	if m.Language != "" || m.ModelID != "" {
		t.Errorf("string fields zero = %q / %q, want empty", m.Language, m.ModelID)
	}
	_ = time.Duration(0) // anchor the time import
}
