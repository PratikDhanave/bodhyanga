package medical

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Provenance is a watermark / signature stamped on a DiagnosisProposal so
// downstream consumers (audit log, eval, replay tools) can answer "which
// model, on which prompt, with which retrieved evidence produced this?"
// without re-running the call.
//
// All fields are JSON-tagged. The whole block is emitted as
// "provenance" on the parent DiagnosisProposal and is `omitempty` so
// rule-based and fixture-style proposals stay compact.
type Provenance struct {
	// Provider is the LLM vendor identifier, e.g. "anthropic" / "openai" /
	// "ollama" / "mock". Sourced from llm.Client.Provider().
	Provider string `json:"provider"`

	// Model is the specific model name, e.g. "claude-sonnet-4-6". Sourced
	// from llm.Client.Model().
	Model string `json:"model"`

	// PromptHash is the SHA-256 of (system + "\n" + userContent) hex-encoded.
	// It's deterministic across runs, so identical prompts produce identical
	// hashes — useful for caching diagnostics and detecting prompt drift.
	PromptHash string `json:"prompt_hash"`

	// CitationIDs lists the Source strings of any RAG citations used.
	// Omitted entirely when no retrieval ran.
	CitationIDs []string `json:"citation_ids,omitempty"`

	// Timestamp is the RFC3339 wall-clock time at which the proposal was
	// produced. UTC.
	Timestamp string `json:"timestamp"`

	// AgentID is the producing agent's stable ID, e.g. "diagnostician".
	AgentID string `json:"agent_id"`

	// Variant identifies the strategy used inside the agent, e.g. "llm",
	// "self-consistency", "debate". Optional.
	Variant string `json:"variant,omitempty"`
}

// HashPrompt returns the SHA-256 hex digest of (system + "\n" + user). The
// delimiter is the literal newline byte; the function is deterministic and
// stable across processes. Exposed for reuse by agents that build their own
// Provenance blocks (self-consistency, debate, vision, …).
func HashPrompt(system, user string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(system))
	_, _ = h.Write([]byte{'\n'})
	_, _ = h.Write([]byte(user))
	return hex.EncodeToString(h.Sum(nil))
}

// MakeProvenance assembles a Provenance record from the ingredients of an
// LLM call. The CitationIDs slice is populated from each citation's Source
// field — the corpus document IDs aren't always exposed to consumers, but
// Source is the human-readable label every retriever already emits.
//
// Timestamp is set to time.Now().UTC() in RFC3339. Callers that need
// deterministic timestamps (tests, replay) should overwrite the field on the
// returned value.
func MakeProvenance(provider, model, system, userContent string, citations []DiagnosisCitation, agentID, variant string) Provenance {
	p := Provenance{
		Provider:   provider,
		Model:      model,
		PromptHash: HashPrompt(system, userContent),
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		AgentID:    agentID,
		Variant:    variant,
	}
	if len(citations) > 0 {
		ids := make([]string, 0, len(citations))
		for _, c := range citations {
			if c.Source != "" {
				ids = append(ids, c.Source)
			}
		}
		if len(ids) > 0 {
			p.CitationIDs = ids
		}
	}
	return p
}
