package analytics

import (
	"testing"
	"time"
)

func TestPriceFor(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		wantIn   float64
		wantOut  float64
		wantCR   float64
	}{
		{"anthropic", "claude-sonnet-4-6", 3.00, 15.00, 0.30},
		{"anthropic", "claude-opus-4-1", 15.00, 75.00, 0.00},
		{"openai", "gpt-4o", 2.50, 10.00, 0.00},
		{"openai", "gpt-4o-mini", 0.15, 0.60, 0.00},
		// Ollama is always free.
		{"ollama", "llama3.2", 0.00, 0.00, 0.00},
		{"ollama", "mistral", 0.00, 0.00, 0.00},
		// Unknown model falls back to zero.
		{"unknown", "mystery-model", 0.00, 0.00, 0.00},
		// Case-insensitive.
		{"Anthropic", "Claude-Sonnet-4-6", 3.00, 15.00, 0.30},
	}
	for _, tc := range tests {
		p := PriceFor(tc.provider, tc.model)
		if p.InputPer1M != tc.wantIn || p.OutputPer1M != tc.wantOut || p.CacheReadPer1M != tc.wantCR {
			t.Errorf("PriceFor(%q, %q) = {%.2f, %.2f, %.2f}, want {%.2f, %.2f, %.2f}",
				tc.provider, tc.model,
				p.InputPer1M, p.OutputPer1M, p.CacheReadPer1M,
				tc.wantIn, tc.wantOut, tc.wantCR)
		}
	}
}

func TestCalcCost(t *testing.T) {
	e := CostEntry{
		Provider:     "anthropic",
		Model:        "claude-sonnet-4-6",
		InputTokens:  1_000_000, // $3.00
		OutputTokens: 1_000_000, // $15.00
		CacheTokens:  1_000_000, // $0.30
	}
	got := CalcCost(e)
	want := 18.30
	if abs(got-want) > 1e-9 {
		t.Errorf("CalcCost = %.4f, want %.4f", got, want)
	}
}

func TestCalcCostOllama(t *testing.T) {
	e := CostEntry{
		Provider:     "ollama",
		Model:        "llama3.2",
		InputTokens:  100_000,
		OutputTokens: 50_000,
	}
	if c := CalcCost(e); c != 0 {
		t.Errorf("CalcCost(ollama) = %f, want 0", c)
	}
}

func TestCostLedger_Record(t *testing.T) {
	l := NewCostLedger()
	now := time.Now().UTC()

	l.Record(CostEntry{
		Timestamp:    now,
		Provider:     "anthropic",
		Model:        "claude-sonnet-4-6",
		AgentID:      "diagnostician",
		TenantID:     "t1",
		InputTokens:  1_000_000,
		OutputTokens: 0,
	})
	l.Record(CostEntry{
		Timestamp:    now,
		Provider:     "openai",
		Model:        "gpt-4o",
		AgentID:      "planner",
		TenantID:     "t1",
		OutputTokens: 1_000_000,
	})

	sum := l.Summarize(now.Add(-time.Minute), now.Add(time.Minute), "t1")
	if sum.TotalCostUSD == 0 {
		t.Error("expected non-zero total cost")
	}
	if sum.ByProvider["anthropic"] == 0 {
		t.Error("expected anthropic cost")
	}
	if sum.ByProvider["openai"] == 0 {
		t.Error("expected openai cost")
	}
	if sum.ByModel["claude-sonnet-4-6"] == 0 {
		t.Error("expected model cost for claude-sonnet-4-6")
	}
}

func TestCostLedger_SummarizeTenantFilter(t *testing.T) {
	l := NewCostLedger()
	now := time.Now().UTC()

	l.Record(CostEntry{
		Timestamp: now, Provider: "openai", Model: "gpt-4o",
		TenantID: "tenant-a", OutputTokens: 1_000_000,
	})
	l.Record(CostEntry{
		Timestamp: now, Provider: "openai", Model: "gpt-4o",
		TenantID: "tenant-b", OutputTokens: 1_000_000,
	})

	sumA := l.Summarize(now.Add(-time.Minute), now.Add(time.Minute), "tenant-a")
	sumAll := l.Summarize(now.Add(-time.Minute), now.Add(time.Minute), "")

	if sumA.TotalCostUSD >= sumAll.TotalCostUSD {
		t.Errorf("tenant-a cost (%.4f) should be less than all-tenant cost (%.4f)",
			sumA.TotalCostUSD, sumAll.TotalCostUSD)
	}
}

func TestCostLedger_TopAgentsBySpend(t *testing.T) {
	l := NewCostLedger()
	now := time.Now().UTC()

	l.Record(CostEntry{
		Timestamp: now, Provider: "anthropic", Model: "claude-sonnet-4-6",
		AgentID: "diag", OutputTokens: 2_000_000,
	})
	l.Record(CostEntry{
		Timestamp: now, Provider: "anthropic", Model: "claude-sonnet-4-6",
		AgentID: "planner", OutputTokens: 1_000_000,
	})
	l.Record(CostEntry{
		Timestamp: now, Provider: "anthropic", Model: "claude-sonnet-4-6",
		AgentID: "intake", OutputTokens: 500_000,
	})

	top := l.TopAgentsBySpend(2)
	if len(top) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(top))
	}
	if top[0] != "diag" {
		t.Errorf("top agent should be 'diag', got %q", top[0])
	}
	if top[1] != "planner" {
		t.Errorf("second agent should be 'planner', got %q", top[1])
	}
}

func TestCostLedger_DailySpend(t *testing.T) {
	l := NewCostLedger()
	now := time.Now().UTC()

	l.Record(CostEntry{
		Timestamp:    now,
		Provider:     "openai",
		Model:        "gpt-4o",
		OutputTokens: 1_000_000,
	})

	daily := l.DailySpend(7)
	if len(daily) != 7 {
		t.Fatalf("expected 7 entries, got %d", len(daily))
	}

	// Last entry is today.
	today := now.Format("2006-01-02")
	last := daily[len(daily)-1]
	if last.Date != today {
		t.Errorf("last entry date = %q, want %q", last.Date, today)
	}
	if last.CostUSD == 0 {
		t.Error("expected non-zero cost for today")
	}

	// All other days should be 0.
	for _, d := range daily[:len(daily)-1] {
		if d.CostUSD != 0 {
			t.Errorf("unexpected cost on %s: %f", d.Date, d.CostUSD)
		}
	}
}

func TestCostSummaryPeriod(t *testing.T) {
	l := NewCostLedger()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	sum := l.Summarize(from, to, "")
	want := "2026-01-01/2026-12-31"
	if sum.Period != want {
		t.Errorf("Period = %q, want %q", sum.Period, want)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
