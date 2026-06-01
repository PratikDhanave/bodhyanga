package analytics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ModelPrice holds the per-1M-token prices for a model.
type ModelPrice struct {
	InputPer1M     float64
	OutputPer1M    float64
	CacheReadPer1M float64
}

// modelPrices is the canonical pricing table (USD per 1 M tokens, as of 2026).
var modelPrices = map[string]ModelPrice{
	"anthropic/claude-sonnet-4-6": {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30},
	"anthropic/claude-opus-4-1":   {InputPer1M: 15.00, OutputPer1M: 75.00},
	"openai/gpt-4o":               {InputPer1M: 2.50, OutputPer1M: 10.00},
	"openai/gpt-4o-mini":          {InputPer1M: 0.15, OutputPer1M: 0.60},
}

// PriceFor returns the ModelPrice for the given provider/model pair.
// Matching is case-insensitive and tolerates provider-prefixed keys or bare
// model names. Ollama (any model) is always $0 — local inference has no token
// billing. Unknown models fall back to zero pricing so accounting never panics.
func PriceFor(provider, model string) ModelPrice {
	p := strings.ToLower(provider)
	m := strings.ToLower(model)

	// Ollama is always free (local inference).
	if p == "ollama" {
		return ModelPrice{}
	}

	// Try provider/model composite key first.
	key := p + "/" + m
	if price, ok := modelPrices[key]; ok {
		return price
	}

	// Fall back to a bare model name match across all providers.
	for k, price := range modelPrices {
		if strings.HasSuffix(k, "/"+m) {
			return price
		}
	}

	return ModelPrice{} // unknown model → zero cost
}

// CostEntry records the token usage and cost for a single LLM call.
type CostEntry struct {
	Timestamp    time.Time
	Provider     string
	Model        string
	AgentID      string
	TenantID     string
	InputTokens  int
	OutputTokens int
	CacheTokens  int
	CostUSD      float64
}

// CostSummary aggregates spend over a time window.
type CostSummary struct {
	TotalCostUSD      float64            `json:"total_cost_usd"`
	ByProvider        map[string]float64 `json:"by_provider"`
	ByModel           map[string]float64 `json:"by_model"`
	ByAgent           map[string]float64 `json:"by_agent"`
	TotalInputTokens  int                `json:"total_input_tokens"`
	TotalOutputTokens int                `json:"total_output_tokens"`
	CostPerCase       float64            `json:"cost_per_case"`
	Period            string             `json:"period"`
}

// CalcCost computes the USD cost for entry e using the canonical price table.
// It overwrites e.CostUSD and also returns the value so callers can use it
// without mutating the entry.
func CalcCost(e CostEntry) float64 {
	price := PriceFor(e.Provider, e.Model)
	cost := float64(e.InputTokens)*price.InputPer1M/1_000_000 +
		float64(e.OutputTokens)*price.OutputPer1M/1_000_000 +
		float64(e.CacheTokens)*price.CacheReadPer1M/1_000_000
	return cost
}

// CostLedger is a thread-safe in-process register of CostEntry records.
type CostLedger struct {
	mu      sync.RWMutex
	entries []CostEntry
}

// NewCostLedger returns an empty CostLedger.
func NewCostLedger() *CostLedger { return &CostLedger{} }

// Record appends e to the ledger. CalcCost is applied so CostUSD is always
// set even if the caller left it at zero.
func (l *CostLedger) Record(e CostEntry) {
	e.CostUSD = CalcCost(e)
	l.mu.Lock()
	l.entries = append(l.entries, e)
	l.mu.Unlock()
}

// Summarize returns a CostSummary over entries whose Timestamp falls in
// [from, to] and whose TenantID matches tenantID. Pass an empty tenantID to
// aggregate across all tenants.
func (l *CostLedger) Summarize(from, to time.Time, tenantID string) CostSummary {
	l.mu.RLock()
	defer l.mu.RUnlock()

	sum := CostSummary{
		ByProvider: map[string]float64{},
		ByModel:    map[string]float64{},
		ByAgent:    map[string]float64{},
		Period:     fmt.Sprintf("%s/%s", from.Format(time.DateOnly), to.Format(time.DateOnly)),
	}
	caseIDs := map[string]struct{}{}

	for _, e := range l.entries {
		if e.Timestamp.Before(from) || e.Timestamp.After(to) {
			continue
		}
		if tenantID != "" && e.TenantID != tenantID {
			continue
		}
		sum.TotalCostUSD += e.CostUSD
		sum.TotalInputTokens += e.InputTokens
		sum.TotalOutputTokens += e.OutputTokens
		sum.ByProvider[e.Provider] += e.CostUSD
		sum.ByModel[e.Model] += e.CostUSD
		if e.AgentID != "" {
			sum.ByAgent[e.AgentID] += e.CostUSD
		}
		if e.AgentID != "" {
			// Use AgentID as a proxy for case-level uniqueness when no explicit
			// case dimension is available.
			caseIDs[e.AgentID] = struct{}{}
		}
	}

	if n := len(caseIDs); n > 0 {
		sum.CostPerCase = sum.TotalCostUSD / float64(n)
	}
	return sum
}

// TopAgentsBySpend returns the IDs of the top n agents ordered by total spend
// (descending). If n ≤ 0 all agents are returned.
func (l *CostLedger) TopAgentsBySpend(n int) []string {
	l.mu.RLock()
	defer l.mu.RUnlock()

	spend := map[string]float64{}
	for _, e := range l.entries {
		if e.AgentID != "" {
			spend[e.AgentID] += e.CostUSD
		}
	}

	agents := make([]string, 0, len(spend))
	for id := range spend {
		agents = append(agents, id)
	}
	sort.Slice(agents, func(i, j int) bool {
		return spend[agents[i]] > spend[agents[j]]
	})

	if n > 0 && n < len(agents) {
		return agents[:n]
	}
	return agents
}

// dailySpendEntry holds one day's aggregated cost.
type dailySpendEntry struct {
	Date    string  `json:"date"`
	CostUSD float64 `json:"cost_usd"`
}

// DailySpend returns a slice of daily totals for the last days calendar days
// (including today), ordered from oldest to newest. Days with no spend are
// included with CostUSD == 0.
func (l *CostLedger) DailySpend(days int) []struct {
	Date    string
	CostUSD float64
} {
	if days <= 0 {
		days = 30
	}

	now := time.Now().UTC()
	// Build an ordered list of the last `days` dates.
	dates := make([]string, days)
	daily := make(map[string]float64, days)
	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -(days - 1 - i)).Format(time.DateOnly)
		dates[i] = d
		daily[d] = 0
	}

	cutoff := now.AddDate(0, 0, -(days - 1))
	cutoff = time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC)

	l.mu.RLock()
	for _, e := range l.entries {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		d := e.Timestamp.UTC().Format(time.DateOnly)
		if _, ok := daily[d]; ok {
			daily[d] += e.CostUSD
		}
	}
	l.mu.RUnlock()

	out := make([]struct {
		Date    string
		CostUSD float64
	}, days)
	for i, d := range dates {
		out[i].Date = d
		out[i].CostUSD = daily[d]
	}
	return out
}
