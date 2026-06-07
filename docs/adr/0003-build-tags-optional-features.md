# ADR 0003: Go Build Tags for Optional Features

**Status:** Accepted

**Date:** 2024-12-22

**Deciders:** Pratik Dhanave

---

## Context

Bodh has two categories of optional dependencies that most deployments do not
need simultaneously:

1. **OpenTelemetry (OTel) tracing and the Laminar exporter.** These pull in
   `go.opentelemetry.io/otel` and its transitive dependency tree (gRPC,
   protobuf, etc.), adding significant binary size and compile time. Many
   deployments -- local development, CI test runs, air-gapped clinic servers
   running Ollama -- do not export traces and gain nothing from these
   dependencies.

2. **Kronk provider integrations.** Kronk is a proprietary audio, LLM, and
   rerank service used in specific enterprise deployments. Its SDK is not
   open-source and should not appear in the default go.sum. Open-source
   contributors cannot (and should not need to) compile against it.

Without build tags, every `go build ./...` would require both dependency trees
to be present, the binary would include code paths that most operators never
activate, and the go.sum would carry proprietary module checksums that
open-source forks cannot reproduce.

The Go ecosystem's standard mechanism for conditional compilation is build
tags (`//go:build <tag>`). The question is how to use them without fracturing
the codebase into "builds with tag X" and "breaks without tag X" variants.

## Decision

Use two build tags -- `otel` and `kronk` -- with a strict stub-file discipline:

### Tag: `otel`

- **Tagged files** (e.g., `pkg/observability/otlp.go`, `laminar.go`) carry
  `//go:build otel` and contain the real OpenTelemetry initialization, span
  creation, and Laminar exporter wiring.

- **Stub files** (e.g., `pkg/observability/otlp_stub.go`,
  `laminar_stub.go`) carry `//go:build !otel` and provide no-op
  implementations of the same exported functions and types. The stubs satisfy
  all call sites so the project compiles and runs cleanly without the tag.

- **Test files** follow the same pattern: `otlp_test.go` (`//go:build otel`)
  tests the real implementation; `otlp_stub.go` ensures the stub compiles.

- **CI runs both:** `make test` runs without tags (exercises stubs),
  `make test-otel` runs with `-tags otel` (exercises real OTel code).

### Tag: `kronk`

- **Tagged files** (e.g., `pkg/llm/kronk/`, `pkg/audio/kronk/`,
  `pkg/knowledge/rerank/kronk/`) carry `//go:build kronk` and contain the
  proprietary adapter implementations.

- **Stub files** (e.g., `kronk_stub.go` in each package) carry
  `//go:build !kronk` and return sentinel errors or no-op values. Every
  agent that could use a Kronk provider receives a stub that satisfies the
  interface contract.

- The go.sum for the default build contains no Kronk SDK checksums.
  Enterprise operators add the Kronk module to their fork's go.mod and
  compile with `-tags kronk`.

### Stub discipline

Every tagged file **must** have a corresponding `_stub.go` counterpart that
exports the same symbols with the negated build constraint. This is enforced
by the CI pipeline: `make ci` runs `go vet ./...` and `go build ./...`
without any tags, which fails if a stub is missing.

### Build invocations

```bash
# Default: no tags, stubs active, small binary
go build ./cmd/care

# OTel tracing enabled
go build -tags otel ./cmd/care

# Kronk proprietary providers enabled
go build -tags kronk ./cmd/care

# Both
go build -tags "otel kronk" ./cmd/care
```

## Consequences

### Positive

- **Default binary stays small.** `go build ./cmd/care` produces a binary
  without OTel's gRPC/protobuf tree or any proprietary SDK. This matters for
  air-gapped deployments with strict binary-audit requirements.

- **Open-source contributors can build and test without proprietary access.**
  `go build ./...` and `make test` succeed with no API keys, no Kronk SDK,
  and no OTel dependencies. The stubs return clean no-op values.

- **Feature parity is guaranteed by stubs.** Because stub files export the
  same function signatures as real files, any call-site change that
  references a new symbol immediately breaks the stub build (caught by CI).
  The tagged and untagged builds cannot drift apart silently.

- **CI covers both paths.** `make test` validates stubs; `make test-otel`
  validates the real OTel code. Both run in the CI pipeline. Kronk-tagged
  tests run only in the enterprise CI with the SDK available.

### Negative

- **Developers must know which tags to use.** A developer debugging a tracing
  issue must remember to build and test with `-tags otel`. Mitigated by
  documenting the tags in CLAUDE.md, the Makefile help target, and the
  README.

- **Stub maintenance is manual.** When a tagged file gains a new exported
  function, the corresponding stub must be updated in the same commit. This
  is a small overhead but a real source of CI failures during development.
  The `go vet` step in `make ci` catches these immediately.

- **Two files per feature boundary.** Each optional feature boundary produces
  two files (real + stub). In `pkg/observability` this means `otlp.go` +
  `otlp_stub.go`, `laminar.go` + `laminar_stub.go`, `trace_handler.go` +
  `trace_handler_stub.go`. The file count is manageable today (six pairs
  total) but would become unwieldy if the number of tagged features grew
  beyond three or four.

### Neutral

- The pattern is standard Go. Build tags and stub files are the idiomatic
  approach used by the Go standard library itself (e.g., `os/exec` stubs
  for unsupported platforms). No custom tooling is needed.

- The Makefile exposes tag-qualified targets (`test-otel`) so developers
  interact with tags through make rather than remembering raw `go build`
  flag syntax.

## References

- `pkg/observability/otlp.go` / `otlp_stub.go` -- OTel OTLP exporter + stub
- `pkg/observability/laminar.go` / `laminar_stub.go` -- Laminar exporter + stub
- `pkg/observability/trace_handler.go` / `trace_handler_stub.go` -- HTTP trace handler + stub
- `pkg/llm/kronk/` -- Kronk LLM provider (tagged)
- `pkg/audio/kronk/` -- Kronk audio provider (tagged)
- `Makefile` -- `test`, `test-otel`, `build` targets
