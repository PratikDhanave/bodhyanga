package llm

import (
	"log"
	"sync"
)

// grammarWarnOnce tracks providers that have already emitted the
// "does not support grammar; ignoring" warning so logs are not spammed when
// an agent processes a batch of cases.
var (
	grammarWarnMu   sync.Mutex
	grammarWarnSeen = map[string]bool{}
)

// warnGrammarUnsupported emits, at most once per provider name, a log line
// noting that Request.Grammar was set but the adapter cannot honour it. The
// adapter proceeds as if Grammar was nil — the JSONSchema path (if set) and
// the agent's retry-on-bad-JSON fallback still run as a backstop.
//
// Used by provider adapters that do not have a native sampler-level grammar
// API (anthropic, openai, ollama as shipped today).
func warnGrammarUnsupported(provider string) {
	grammarWarnMu.Lock()
	defer grammarWarnMu.Unlock()
	if grammarWarnSeen[provider] {
		return
	}
	grammarWarnSeen[provider] = true
	log.Printf("llm/%s: provider does not support grammar-constrained generation; "+
		"ignoring Request.Grammar (JSONSchema path and agent retry-on-bad-JSON still apply)", provider)
}

// resetGrammarWarnSeenForTest clears the "warned providers" set so tests can
// assert the warning fires on first use without pollution from earlier cases.
// Package-private and intended for use from grammar_test.go only.
func resetGrammarWarnSeenForTest() {
	grammarWarnMu.Lock()
	defer grammarWarnMu.Unlock()
	grammarWarnSeen = map[string]bool{}
}
