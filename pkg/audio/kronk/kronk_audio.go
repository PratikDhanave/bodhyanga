// Package kronk wraps the Ardan Labs kronk SDK to provide an on-prem
// Whisper-style audio transcriber for Bodhyanga. This file is compiled ONLY
// when the `kronk` build tag is set (e.g. `go build -tags kronk
// ./cmd/care`). The default Bodhyanga build does not link the kronk runtime
// — see kronk_audio_stub.go for the no-op constructor used otherwise.
//
// Wiring (cmd/care/main.go, also under `//go:build kronk`):
//
//	t, err := kronkaudio.NewTranscriber(ctx, kronkaudio.Options{
//	    ModelSource: "mradermacher/Qwen2-Audio-7B.Q8_0",
//	})
//	if err != nil { log.Fatal(err) }
//	defer t.Close()
//	transcriber = t  // pkg/audio.Transcriber
//
// PHI policy: this file processes audio bytes and produces transcribed
// text. BOTH are PHI. They never leave the process — kronk runs the
// model in-process — and they are never logged. Meta returned to the
// caller is PHI-free (model_id, duration, confidence, language).
//
// Mirror of https://github.com/ardanlabs/kronk/blob/main/examples/audio/main.go.

//go:build kronk

package kronk

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/audio"

	// kronk is the upstream Ardan Labs SDK. Adding it to go.mod is gated
	// behind this build tag so the default `go.sum` stays tiny.
	"github.com/ardanlabs/kronk"
)

// DefaultModelSource is the audio model used when Options.ModelSource is
// empty. Matches the kronk audio example.
const DefaultModelSource = "mradermacher/Qwen2-Audio-7B.Q8_0"

// Options configures the kronk-backed Transcriber.
type Options struct {
	// ModelSource is the kronk model identifier (Hugging Face repo /
	// quantization tag). Defaults to DefaultModelSource.
	ModelSource string

	// Prompt is an optional instruction sent alongside the audio. The
	// kronk audio example passes a "transcribe this audio" prompt;
	// callers can override for language or style hints. Must never
	// embed PHI — this string is sent into the model context.
	Prompt string

	// Timeout caps each Transcribe call. Zero means use the caller's
	// ctx deadline.
	Timeout time.Duration
}

// Transcriber is the kronk-backed implementation of pkg/audio.Transcriber.
// Construct via NewTranscriber. Always call Close on shutdown to release
// the model handle.
type Transcriber struct {
	model       *kronk.AudioModel
	modelID     string
	defaultLang string
	prompt      string
	timeout     time.Duration
}

// NewTranscriber loads the kronk audio model and returns a Transcriber.
// The first call may take several seconds (model download/quantization);
// subsequent calls reuse the loaded weights. Returns an error if the
// model can't be opened — caller is expected to fall back to a different
// backend or surface ErrUnavailable to the agent.
func NewTranscriber(ctx context.Context, opts Options) (*Transcriber, error) {
	if opts.ModelSource == "" {
		opts.ModelSource = DefaultModelSource
	}
	if opts.Prompt == "" {
		opts.Prompt = "Transcribe the spoken audio into text. Output only the transcription, no commentary."
	}
	// kronk.NewAudioModel mirrors the upstream example: it loads the
	// quantized weights into memory and returns a handle that can be
	// reused across calls.
	model, err := kronk.NewAudioModel(ctx, opts.ModelSource)
	if err != nil {
		return nil, fmt.Errorf("kronk: load audio model %q: %w", opts.ModelSource, err)
	}
	return &Transcriber{
		model:       model,
		modelID:     opts.ModelSource,
		defaultLang: "en",
		prompt:      opts.Prompt,
		timeout:     opts.Timeout,
	}, nil
}

// Transcribe runs the kronk audio model on the given bytes. mediaType is
// passed to kronk so it can pick the right decoder (wav, mp3, flac).
//
// Unsupported media types return audio.ErrUnsupportedMedia. Model load /
// inference failures return a wrapped error; the agent maps either to
// ErrUnavailable so the clinician's caller falls back to typed notes.
func (t *Transcriber) Transcribe(ctx context.Context, audioBytes []byte, mediaType string) (string, audio.TranscriptionMeta, error) {
	if !supportedMedia(mediaType) {
		return "", audio.TranscriptionMeta{}, audio.ErrUnsupportedMedia
	}
	if t.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}

	start := time.Now()
	// kronk.AudioModel.Transcribe is the upstream example's call
	// shape: bytes in, text out, with an optional prompt argument.
	resp, err := t.model.Transcribe(ctx, audioBytes, kronk.AudioOptions{
		Prompt:    t.prompt,
		MediaType: mediaType,
	})
	if err != nil {
		return "", audio.TranscriptionMeta{}, fmt.Errorf("kronk: transcribe: %w", err)
	}

	meta := audio.TranscriptionMeta{
		Duration:   resp.Duration,
		Confidence: resp.Confidence,
		Language:   resp.Language,
		ModelID:    t.modelID,
	}
	if meta.Language == "" {
		meta.Language = t.defaultLang
	}
	// Sanity-clamp confidence to [0,1] in case the upstream returns a
	// value outside the contract. The clamp is silent — we never want
	// to fail a transcription on a meta-field oddity.
	if meta.Confidence < 0 {
		meta.Confidence = 0
	}
	if meta.Confidence > 1 {
		meta.Confidence = 1
	}
	// If kronk doesn't report duration we leave it zero so the audit
	// log's omitempty drops the field rather than recording an
	// inaccurate measurement. The wall-clock elapsed is intentionally
	// NOT used as a fallback duration — it would conflate processing
	// time with audio length.
	_ = start

	return resp.Text, meta, nil
}

// Close releases the model handle. Safe to call multiple times.
func (t *Transcriber) Close() error {
	if t == nil || t.model == nil {
		return nil
	}
	err := t.model.Close()
	t.model = nil
	return err
}

// supportedMedia returns true for the audio MIME types kronk's decoders
// handle. The set is conservative on purpose — adding a new type without
// testing the decoder against representative clinical dictations is how
// "silent corruption" sneaks into clinical text.
func supportedMedia(mt string) bool {
	mt = strings.ToLower(strings.TrimSpace(mt))
	// Strip charset / codec suffixes (e.g. "audio/wav; codecs=...")
	if i := strings.IndexByte(mt, ';'); i >= 0 {
		mt = strings.TrimSpace(mt[:i])
	}
	switch mt {
	case "audio/wav", "audio/wave", "audio/x-wav",
		"audio/mpeg", "audio/mp3",
		"audio/flac", "audio/x-flac",
		"audio/ogg":
		return true
	}
	return false
}

// Compile-time assertion that Transcriber satisfies audio.Transcriber.
var _ audio.Transcriber = (*Transcriber)(nil)
