package llm

// Grammar contract — sampler-level structured-output guarantee.
//
// Two layers of structured-output enforcement live in this package:
//
//  1. Request.JSONSchema — a JSON Schema the provider is asked to respect.
//     Wired to Anthropic tool-use, OpenAI strict json_schema, and Ollama
//     format. Validation is done at the API boundary; a malformed response
//     surfaces as ErrInvalidJSON and the agent's fallback path runs.
//
//  2. Request.Grammar (GrammarSpec) — a SAMPLER-LEVEL constraint. When the
//     provider supports it (llama.cpp-family backends — kronk in Bodhyanga today),
//     the sampler refuses any token that would break the grammar, so the
//     response is GUARANTEED well-formed without a retry. Providers that do
//     not support grammars emit a one-time warning and ignore the field;
//     the JSONSchema and retry paths still run as a backstop.
//
// Why both? GBNF gives a hard structural guarantee but is verbose for nested
// fields; JSON Schema is concise and covers field types but relies on the
// model behaving. The two layers compose: send GBNF for the outer shape
// (object / enum / regex) and JSON Schema for the field-level types and
// required-fields list.
//
// Provider support today:
//
//	Provider       Grammar         JSONSchema
//	anthropic      ignored (warn)  yes (tool-use)
//	openai         ignored (warn)  yes (strict json_schema)
//	ollama         ignored (warn)  yes (format)
//	kronk          yes (GBNF)      yes (best-effort)
//	mock           recorded        yes
//
// Helpers — GBNFJSONObject, GBNFEnum, GBNFRegex — return ready-to-use specs
// for the three most common cases. Agents define their grammar as a `const`
// in the same file as the prompt template, kept in lockstep with the JSON
// Schema. See agents/diagnostician_llm.go for the canonical pattern.
