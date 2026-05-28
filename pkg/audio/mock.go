package audio

import (
	"context"
	"time"
)

// MockTranscriber returns a canned transcript and meta. Used by tests and
// by cmd/care when the binary is built without the kronk tag. It is NOT a
// rule-based fallback for the agent — the rule fallback is "return an
// `audio path unavailable` error" (see ErrUnavailable). MockTranscriber
// exists solely so the agent + HTTP handler are exercisable end-to-end
// without a model runtime.
//
// All fields are exported so tests can swap in their own canned values
// (per-test fixtures, error injection via Err) without subclassing.
type MockTranscriber struct {
	// Text is the canned transcript returned by Transcribe. Empty
	// string is valid and tests use it to simulate "model returned
	// nothing".
	Text string

	// Meta is the canned TranscriptionMeta. ModelID defaults to "mock"
	// in NewMock when callers don't set it explicitly, so the audit
	// log always carries a recognisable label.
	Meta TranscriptionMeta

	// Err, if non-nil, is returned by Transcribe instead of (Text,
	// Meta). Tests use this to exercise the agent's error path
	// (ErrUnavailable in particular).
	Err error

	// CallCount is incremented on every Transcribe call. Useful for
	// tests asserting that the agent invoked the transcriber exactly
	// once per request.
	CallCount int
}

// NewMock returns a MockTranscriber pre-populated with a small canned
// transcript. The text is intentionally non-clinical ("note dictated for
// case ...") so accidental leaks in tests do not look like PHI.
func NewMock() *MockTranscriber {
	return &MockTranscriber{
		Text: "note dictated for case under test",
		Meta: TranscriptionMeta{
			Duration:   3 * time.Second,
			Confidence: 0.92,
			Language:   "en",
			ModelID:    "mock",
		},
	}
}

// Transcribe implements Transcriber. It honours ctx so cancellation tests
// pass; otherwise it returns the canned values immediately.
func (m *MockTranscriber) Transcribe(ctx context.Context, audio []byte, mediaType string) (string, TranscriptionMeta, error) {
	m.CallCount++
	if err := ctx.Err(); err != nil {
		return "", TranscriptionMeta{}, err
	}
	if m.Err != nil {
		return "", TranscriptionMeta{}, m.Err
	}
	meta := m.Meta
	if meta.ModelID == "" {
		meta.ModelID = "mock"
	}
	return m.Text, meta, nil
}

// Compile-time assertion that MockTranscriber satisfies Transcriber.
var _ Transcriber = (*MockTranscriber)(nil)
