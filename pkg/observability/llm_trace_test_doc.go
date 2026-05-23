// llm_trace_test.go — unit tests for LLMTrace emission and OTel hook gating.
//
// Overview:
//   Exercises the JSON-line output path of (*StdLogger).RecordLLMCall using a
//   bytes.Buffer-backed logger built via NewLoggerTo, plus the
//   environment-gated OTel hook. Tests reset SetLLMOTelHook(nil) and
//   unset BODH_OTEL_ENABLED in t.Cleanup to stay isolated.
//
// Key declarations:
//   - TestRecordLLMCall_WritesJSONLine: confirms the "[llm-trace]" tag and that the JSON round-trips.
//   - TestRecordLLMCall_NormalisesKind: empty Kind is filled with "completion".
//   - TestSetLLMOTelHook_FiresOnlyWhenEnvSet: hook is skipped without the env var, fires once with it.
//
// See doc.go for package-level documentation.

package observability
