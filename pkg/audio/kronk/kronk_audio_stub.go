// kronk_audio_stub.go — no-op shim used when the `kronk` build tag is NOT
// set. Bodh's default build does not link the kronk SDK; this stub keeps
// the Transcriber type and constructor compiling so callers (cmd/care,
// any consumer) can reference them unconditionally.
//
// To get real kronk transcription: build with `go build -tags kronk
// ./cmd/care` and ensure `github.com/ardanlabs/kronk` is in go.mod.

//go:build !kronk

package kronk

import (
	"context"
	"errors"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audio"
)

// DefaultModelSource matches the real implementation's default so callers
// can read it for diagnostics without needing the build tag.
const DefaultModelSource = "mradermacher/Qwen2-Audio-7B.Q8_0"

// Options mirrors the real implementation's options struct so callers can
// construct it unconditionally. Fields are ignored in the stub.
type Options struct {
	ModelSource string
	Prompt      string
	Timeout     time.Duration
}

// Transcriber is a no-op stub. Calls return audio.ErrUnavailable so the
// agent's deterministic fallback path engages.
type Transcriber struct{}

// NewTranscriber returns an error in the default build to signal the
// kronk SDK was not linked. Build with `-tags kronk` for the real
// transcriber. The error wraps audio.ErrUnavailable so callers can
// errors.Is-check it on the same predicate they use for transient
// runtime failures.
func NewTranscriber(_ context.Context, _ Options) (*Transcriber, error) {
	return nil, errors.New("audio/kronk: rebuild with -tags=kronk to enable on-prem audio transcription: " + audio.ErrUnavailable.Error())
}

// Transcribe is a no-op that always returns audio.ErrUnavailable.
func (t *Transcriber) Transcribe(_ context.Context, _ []byte, _ string) (string, audio.TranscriptionMeta, error) {
	return "", audio.TranscriptionMeta{}, audio.ErrUnavailable
}

// Close is a no-op stub.
func (t *Transcriber) Close() error { return nil }

// Compile-time assertion that the stub satisfies audio.Transcriber so a
// drift in the interface fails to compile here too.
var _ audio.Transcriber = (*Transcriber)(nil)
