// Package kronk — Ardan Labs kronk (in-process llama.cpp) adapter for
// the Bodh provider-agnostic llm.Client interface.
//
// Overview:
//
//	Implements pkg/llm.Client by driving Ardan Labs' kronk library, which
//	wraps llama.cpp (via yzma + bucky) for direct CGO inference inside the
//	Go binary. No daemon, no network, no API keys — the model runs in the
//	same process as the rest of Bodh. This is the HIPAA-friendly path:
//	no PHI ever leaves the box, because there is no "box" to leave.
//
// Build tag:
//
//	Every file in this package carries `//go:build kronk` (or
//	`//go:build !kronk` for the stub). The default Bodh build is unaffected:
//	no llama.cpp, no yzma, no bucky, no CGO. To enable, build with
//
//	  go build -tags kronk ./cmd/care
//
//	and the constructor returned by NewClient links the native dependencies.
//	This mirrors the pkg/observability/otel pattern: large or platform-
//	specific deps are gated behind a tag so go.sum stays small.
//
// Key declarations:
//   - ProviderName: "kronk" (returned by Client.Provider()).
//   - DefaultModelSource: "unsloth/Qwen3-0.6B-Q8_0" (kronk example's default).
//   - Tracer interface: minimal sink consumed by Complete to emit one
//     observability.LLMTrace per call. *observability.StdLogger satisfies it.
//   - New(modelSource, logger, tracer, opts...) (*Client, error): constructor.
//     Returns an error when the model cannot be loaded; callers fall back
//     to the deterministic rule path.
//   - (*Client).Complete: translates llm.Request to kronk.ChatRequest, runs
//     the inference, validates JSON-schema responses client-side, and
//     records one trace.
//   - (*Client).Close: releases the kronk backend; defer after a successful
//     New.
//   - WithGrammar(grammar): functional option carrying a GBNF grammar — the
//     grammar track populates this later; the public API is stable today.
//
// Fallback contract:
//
//	This provider, like every LLM-backed agent in Bodh, must be wired
//	behind a deterministic rule-based fallback (see AGENTS.md invariant #3).
//	The agents/diagnostician_factory.go arm for "llm-kronk" wraps the
//	client in LLMDiagnostician with DiagnosticianAgent as the Fallback.
//
// See kronk_stub.go for the default-build no-op shim.
package kronk
