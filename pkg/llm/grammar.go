package llm

import (
	"fmt"
	"strings"
)

// GrammarFormat names the dialect the GrammarSpec.Body is written in. Different
// providers honour different dialects natively, so the format tag tells the
// adapter how (or whether) to wire the body to its API.
type GrammarFormat string

const (
	// GrammarGBNF is llama.cpp's GGUF Backus-Naur Form. Used by llama.cpp,
	// kronk, and any other provider that supports sampler-level constrained
	// decoding. The body is a GBNF document — see
	// https://github.com/ggerganov/llama.cpp/blob/master/grammars/README.md
	// for the dialect reference.
	GrammarGBNF GrammarFormat = "gbnf"

	// GrammarJSONSchema is a JSON Schema document. Useful when the caller
	// wants the same structural guarantee but the underlying provider only
	// supports JSON Schema (OpenAI strict json_schema, Anthropic tool-use).
	// Adapters MAY translate GBNF → JSON Schema on a best-effort basis, but
	// the supported path is: send GBNF to grammar-aware providers, send
	// JSONSchema (the existing Request.JSONSchema field) to schema-aware
	// providers. Mixing the two in one request is undefined.
	GrammarJSONSchema GrammarFormat = "json_schema"
)

// GrammarSpec is a sampler-level constraint on the model's output. Set
// Request.Grammar to a non-nil GrammarSpec and the adapter will, if the
// provider supports it, refuse any token that breaks the grammar — meaning
// the response is guaranteed well-formed without a retry loop.
//
// Providers that do NOT support grammars (today: Anthropic, OpenAI, Ollama
// in the adapters that ship in this package) will log a one-time warning and
// ignore the field. The existing JSONSchema path and the agent-level
// retry-on-bad-JSON fallback still run as a backstop.
//
// The kronk provider (a separate track) honours GBNF natively via llama.cpp.
type GrammarSpec struct {
	// Format selects the dialect. Required.
	Format GrammarFormat

	// Body is the grammar document. For GrammarGBNF this is GBNF source
	// text starting with a `root ::= ...` rule. For GrammarJSONSchema this
	// is the JSON Schema document.
	Body string
}

// GBNFJSONObject returns a permissive GBNF grammar that constrains output to
// any valid JSON object (curly-brace document). Use this when the caller wants
// the sampler-level "must be a JSON object" guarantee but is happy to keep
// the field-level validation in the existing JSONSchema path. Pair with
// Request.JSONSchema to get both the structural guarantee from GBNF and the
// schema-level validation downstream agents already perform.
//
// The grammar deliberately does not constrain field names or value types —
// that's what JSONSchema is for. Think of this as "syntactically valid JSON
// object" only.
func GBNFJSONObject() GrammarSpec {
	// Standard JSON GBNF adapted from llama.cpp's bundled grammars/json.gbnf.
	// Kept short and self-contained so the grammar is testable as a single
	// string constant.
	const body = `root   ::= object
object ::= "{" ws ( pair ( ws "," ws pair )* )? ws "}"
pair   ::= string ws ":" ws value
value  ::= object | array | string | number | "true" | "false" | "null"
array  ::= "[" ws ( value ( ws "," ws value )* )? ws "]"
string ::= "\"" ( [^"\\] | "\\" . )* "\""
number ::= "-"? ( "0" | [1-9] [0-9]* ) ( "." [0-9]+ )? ( [eE] [-+]? [0-9]+ )?
ws     ::= [ \t\n\r]*
`
	return GrammarSpec{Format: GrammarGBNF, Body: body}
}

// GBNFEnum returns a GBNF grammar whose root accepts exactly one of the
// supplied string literals (wrapped in double quotes, JSON-string style).
// Use this for classifier-style agents whose output must be exactly one of
// a fixed vocabulary — e.g. inbox triage categories, verifier verdicts.
//
// The returned grammar yields a bare JSON string (e.g. "refill") at the root.
// If you want the value embedded in an object — e.g. `{"category":"refill"}`
// — wrap GBNFEnum with your own grammar or use GBNFJSONObject and validate
// the field downstream via JSONSchema.
//
// Empty input panics — there is no valid enum with zero alternatives.
func GBNFEnum(values []string) GrammarSpec {
	if len(values) == 0 {
		panic("llm.GBNFEnum: values must not be empty")
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, `"\"`+escapeGBNFString(v)+`\""`)
	}
	body := "root ::= " + strings.Join(parts, " | ") + "\n"
	return GrammarSpec{Format: GrammarGBNF, Body: body}
}

// GBNFRegex returns a GBNF grammar whose root matches a single character-class
// based pattern. Useful for narrow patterns the model must hit exactly —
// e.g. ICD-10 codes ("[A-Z][0-9]{2}(\\.[0-9]{1,4})?") or a date stamp.
//
// The implementation embeds the pattern as a `root` rule using GBNF's regex-
// inspired character-class syntax. NOTE: GBNF is NOT full regex — it only
// supports literals, `[...]` character classes, `?`, `*`, `+`, and `|`. The
// caller is responsible for staying inside that subset. The Body is wrapped
// in a single root rule so the result composes with other GBNF documents.
func GBNFRegex(pattern string) GrammarSpec {
	body := fmt.Sprintf("root ::= %s\n", pattern)
	return GrammarSpec{Format: GrammarGBNF, Body: body}
}

// escapeGBNFString escapes the characters that have special meaning inside a
// GBNF double-quoted string literal. GBNF treats `"` and `\` as the only
// special characters inside a string literal, so this is conservative but
// sufficient for the JSON-string-of-enum-value case.
func escapeGBNFString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
