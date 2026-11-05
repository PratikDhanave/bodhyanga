package analytics

import (
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/eval"
)

// --- SummarizeCases: empty records ---

func TestSummarizeCases_Empty(t *testing.T) {
	m := SummarizeCases(nil)
	if m.Cases != 0 {
		t.Errorf("Cases = %d, want 0", m.Cases)
	}
}

// --- PriceFor: bare model name match without provider prefix ---

func TestPriceFor_BareModelNameFallback(t *testing.T) {
	// Use a different provider string that won't match the composite key
	// but the model name will match via the HasSuffix fallback.
	p := PriceFor("custom", "gpt-4o")
	if p.InputPer1M != 2.50 {
		t.Errorf("InputPer1M = %f, want 2.50 (bare model match)", p.InputPer1M)
	}
}

// --- SummarizeCases: metadata under_budget non-bool ---

func TestSummarizeCases_MetadataNotBool(t *testing.T) {
	records := []eval.InteractionRecord{
		{
			Metrics:  map[string]float64{"accuracy": 0.9, "cost_usd": 0.5, "rpm_alert_count": 0},
			Metadata: map[string]any{"under_budget": "yes"}, // string, not bool
		},
	}
	m := SummarizeCases(records)
	if m.UnderBudgetPct != 0 {
		t.Errorf("UnderBudgetPct = %f, want 0 (non-bool metadata)", m.UnderBudgetPct)
	}
}

func TestSummarizeCases_NilMetadata(t *testing.T) {
	records := []eval.InteractionRecord{
		{
			Metrics:  map[string]float64{"accuracy": 0.8, "cost_usd": 0.3},
			Metadata: nil,
		},
	}
	m := SummarizeCases(records)
	if m.Cases != 1 {
		t.Errorf("Cases = %d, want 1", m.Cases)
	}
}

// --- PriceFor: unknown model fallback ---

func TestPriceFor_UnknownModel(t *testing.T) {
	p := PriceFor("unknown", "nonexistent-model")
	if p.InputPer1M != 0 || p.OutputPer1M != 0 {
		t.Errorf("unknown model should return zero pricing, got %+v", p)
	}
}

// --- PriceFor: bare model name match ---

func TestPriceFor_BareModelMatch(t *testing.T) {
	// Should find "anthropic/claude-sonnet-4-6" by bare model name.
	p := PriceFor("anthropic", "claude-sonnet-4-6")
	if p.InputPer1M != 3.00 {
		t.Errorf("InputPer1M = %f, want 3.00", p.InputPer1M)
	}
}

// --- Summarize: filter by tenant ---

func TestSummarize_TenantFilter(t *testing.T) {
	l := NewCostLedger()
	now := time.Now().UTC()
	l.Record(CostEntry{
		Timestamp: now, Provider: "anthropic", Model: "claude-sonnet-4-6",
		AgentID: "a1", TenantID: "t1", InputTokens: 1000, OutputTokens: 500,
	})
	l.Record(CostEntry{
		Timestamp: now, Provider: "anthropic", Model: "claude-sonnet-4-6",
		AgentID: "a2", TenantID: "t2", InputTokens: 2000, OutputTokens: 1000,
	})

	sum := l.Summarize(now.Add(-time.Hour), now.Add(time.Hour), "t1")
	if sum.TotalInputTokens != 1000 {
		t.Errorf("TotalInputTokens = %d, want 1000 (filtered to t1)", sum.TotalInputTokens)
	}
}

// --- Summarize: entries outside time range ---

func TestSummarize_OutsideTimeRange(t *testing.T) {
	l := NewCostLedger()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	l.Record(CostEntry{
		Timestamp: old, Provider: "openai", Model: "gpt-4o",
		AgentID: "a1", TenantID: "t1", InputTokens: 1000,
	})

	now := time.Now().UTC()
	sum := l.Summarize(now.Add(-time.Hour), now.Add(time.Hour), "")
	if sum.TotalInputTokens != 0 {
		t.Errorf("expected 0 tokens outside time range, got %d", sum.TotalInputTokens)
	}
}

// --- TopAgentsBySpend: empty entries ---

func TestTopAgentsBySpend_NoAgentID(t *testing.T) {
	l := NewCostLedger()
	l.Record(CostEntry{
		Timestamp: time.Now(), Provider: "ollama", Model: "llama3",
		InputTokens: 1000, // no AgentID
	})
	top := l.TopAgentsBySpend(5)
	if len(top) != 0 {
		t.Errorf("expected 0 agents (no AgentID), got %d", len(top))
	}
}

// --- TopAgentsBySpend: n=0 returns all ---

func TestTopAgentsBySpend_NZeroReturnsAll(t *testing.T) {
	l := NewCostLedger()
	now := time.Now()
	l.Record(CostEntry{Timestamp: now, Provider: "anthropic", Model: "claude-sonnet-4-6", AgentID: "a1", InputTokens: 100})
	l.Record(CostEntry{Timestamp: now, Provider: "anthropic", Model: "claude-sonnet-4-6", AgentID: "a2", InputTokens: 200})
	top := l.TopAgentsBySpend(0)
	if len(top) != 2 {
		t.Errorf("expected 2 agents, got %d", len(top))
	}
}

// --- DailySpend: days=0 defaults to 30 ---

func TestDailySpend_DefaultDays(t *testing.T) {
	l := NewCostLedger()
	result := l.DailySpend(0)
	if len(result) != 30 {
		t.Errorf("expected 30 days (default), got %d", len(result))
	}
}

// --- DailySpend: entries before cutoff excluded ---

func TestDailySpend_OldEntriesExcluded(t *testing.T) {
	l := NewCostLedger()
	old := time.Now().UTC().AddDate(0, 0, -60)
	l.Record(CostEntry{
		Timestamp: old, Provider: "openai", Model: "gpt-4o",
		InputTokens: 1000, OutputTokens: 500,
	})
	result := l.DailySpend(7)
	totalCost := 0.0
	for _, d := range result {
		totalCost += d.CostUSD
	}
	if totalCost != 0 {
		t.Errorf("expected 0 cost (old entries), got %f", totalCost)
	}
}
