package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// budgetMock is a minimal LLM client for budget routing tests. It records
// calls and can be configured to fail.
type budgetMock struct {
	provider string
	model    string
	resp     *Response
	err      error
	calls    int
}

func (m *budgetMock) Provider() string { return m.provider }
func (m *budgetMock) Model() string    { return m.model }
func (m *budgetMock) Complete(_ context.Context, _ Request) (*Response, error) {
	m.calls++
	if m.err != nil {
		return nil, m.err
	}
	if m.resp != nil {
		return m.resp, nil
	}
	return &Response{
		Provider:     m.provider,
		Model:        m.model,
		InputTokens:  100,
		OutputTokens: 50,
	}, nil
}

// mockLedger is a test implementation of SpendLedger that lets tests
// control reported spend and observe recorded usage.
type mockLedger struct {
	mu    sync.Mutex
	spend float64 // value returned by MonthlySpendUSD

	// recorded captures every RecordUsage call for assertions.
	recorded []mockUsage
}

type mockUsage struct {
	provider, model              string
	inputTokens, outputTokens    int
	cacheTokens                  int
}

func (l *mockLedger) MonthlySpendUSD(_, _ time.Time) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spend
}

func (l *mockLedger) RecordUsage(provider, model string, inputTokens, outputTokens, cacheTokens int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recorded = append(l.recorded, mockUsage{
		provider:     provider,
		model:        model,
		inputTokens:  inputTokens,
		outputTokens: outputTokens,
		cacheTokens:  cacheTokens,
	})
}

func (l *mockLedger) setSpend(v float64) {
	l.mu.Lock()
	l.spend = v
	l.mu.Unlock()
}

// newTestTiers builds three tiers: cheap (ollama), mid (openai), expensive (anthropic).
func newTestTiers() (cheap, mid, expensive *budgetMock, tiers []TierConfig) {
	cheap = &budgetMock{
		provider: "ollama",
		model:    "llama3.2",
		resp:     &Response{Provider: "ollama", Model: "llama3.2", InputTokens: 100, OutputTokens: 50},
	}
	mid = &budgetMock{
		provider: "openai",
		model:    "gpt-4o-mini",
		resp:     &Response{Provider: "openai", Model: "gpt-4o-mini", InputTokens: 100, OutputTokens: 50},
	}
	expensive = &budgetMock{
		provider: "anthropic",
		model:    "claude-sonnet-4-6",
		resp:     &Response{Provider: "anthropic", Model: "claude-sonnet-4-6", InputTokens: 100, OutputTokens: 50},
	}
	tiers = []TierConfig{
		{Name: "local", Provider: "ollama", Model: "llama3.2", Client: cheap},
		{Name: "small-cloud", Provider: "openai", Model: "gpt-4o-mini", Client: mid},
		{Name: "large-cloud", Provider: "anthropic", Model: "claude-sonnet-4-6", Client: expensive},
	}
	return
}

func TestBudgetRouter_UnderBudgetRoutesToDefault(t *testing.T) {
	t.Parallel()
	cheap, mid, expensive, tiers := newTestTiers()
	ledger := &mockLedger{spend: 0} // no spend

	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100.0,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      2, // expensive tier
	}, ledger)

	req := Request{
		System:    "test",
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		MaxTokens: 100,
	}

	resp, err := router.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Provider != "anthropic" {
		t.Fatalf("expected provider=anthropic, got %s", resp.Provider)
	}
	if expensive.calls != 1 {
		t.Fatalf("expensive.calls=%d, want 1", expensive.calls)
	}
	if mid.calls != 0 {
		t.Fatalf("mid.calls=%d, want 0", mid.calls)
	}
	if cheap.calls != 0 {
		t.Fatalf("cheap.calls=%d, want 0", cheap.calls)
	}

	// No alerts should have been emitted.
	if alerts := router.Alerts(); len(alerts) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(alerts))
	}

	// RemainingBudget should equal the full limit (ledger reports $0 spent).
	remaining := router.RemainingBudget()
	if remaining != 100.0 {
		t.Fatalf("remaining=%f, expected 100.0", remaining)
	}

	// Usage should have been recorded.
	ledger.mu.Lock()
	n := len(ledger.recorded)
	ledger.mu.Unlock()
	if n != 1 {
		t.Fatalf("expected 1 usage record, got %d", n)
	}
}

func TestBudgetRouter_OverWarningThresholdRoutesToCheapest(t *testing.T) {
	t.Parallel()
	cheap, _, _, tiers := newTestTiers()
	ledger := &mockLedger{spend: 8.5} // 85% of $10 limit

	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  10.0,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      2,
	}, ledger)

	var alertReceived []BudgetAlert
	router.SetAlertCallback(func(a BudgetAlert) {
		alertReceived = append(alertReceived, a)
	})

	req := Request{
		System:    "test",
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		MaxTokens: 100,
	}

	resp, err := router.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Provider != "ollama" {
		t.Fatalf("expected provider=ollama (cheapest), got %s", resp.Provider)
	}
	if cheap.calls != 1 {
		t.Fatalf("cheap.calls=%d, want 1", cheap.calls)
	}

	// Should have received a warning alert.
	alerts := router.Alerts()
	foundWarning := false
	for _, a := range alerts {
		if a.Type == "warning" {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatalf("expected a 'warning' alert, got alerts: %+v", alerts)
	}

	// Callback should also have been invoked.
	if len(alertReceived) == 0 {
		t.Fatal("alert callback was not invoked")
	}
}

func TestBudgetRouter_OverHardLimitRoutesToCheapestWithAlert(t *testing.T) {
	t.Parallel()
	cheap, _, _, tiers := newTestTiers()
	ledger := &mockLedger{spend: 12.50} // over $10 limit

	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  10.0,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      2,
	}, ledger)

	req := Request{
		System:    "test",
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		MaxTokens: 100,
	}

	resp, err := router.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Provider != "ollama" {
		t.Fatalf("expected provider=ollama, got %s", resp.Provider)
	}
	if cheap.calls != 1 {
		t.Fatalf("cheap.calls=%d, want 1", cheap.calls)
	}

	// Should have a hard_limit alert.
	alerts := router.Alerts()
	foundHardLimit := false
	for _, a := range alerts {
		if a.Type == "hard_limit" {
			foundHardLimit = true
			if a.SpentUSD < 10.0 {
				t.Fatalf("hard_limit alert SpentUSD=%f, expected >= 10.0", a.SpentUSD)
			}
		}
	}
	if !foundHardLimit {
		t.Fatalf("expected a 'hard_limit' alert, got alerts: %+v", alerts)
	}

	// RemainingBudget should be negative.
	if remaining := router.RemainingBudget(); remaining >= 0 {
		t.Fatalf("remaining=%f, expected negative when over limit", remaining)
	}
}

func TestBudgetRouter_CascadingFallbackOnTierFailure(t *testing.T) {
	t.Parallel()
	cheap, mid, expensive, tiers := newTestTiers()

	// Make the expensive and mid tiers fail.
	expensive.err = errors.New("anthropic: rate limited")
	mid.err = errors.New("openai: server error")

	ledger := &mockLedger{spend: 0}
	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100.0,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      2, // start at expensive
	}, ledger)

	req := Request{
		System:    "test",
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		MaxTokens: 100,
	}

	resp, err := router.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Provider != "ollama" {
		t.Fatalf("expected fallback to ollama, got %s", resp.Provider)
	}

	// All three tiers should have been tried.
	if expensive.calls != 1 {
		t.Fatalf("expensive.calls=%d, want 1", expensive.calls)
	}
	if mid.calls != 1 {
		t.Fatalf("mid.calls=%d, want 1", mid.calls)
	}
	if cheap.calls != 1 {
		t.Fatalf("cheap.calls=%d, want 1", cheap.calls)
	}

	// Should have tier_escalation alerts for each fallback step.
	alerts := router.Alerts()
	escalations := 0
	for _, a := range alerts {
		if a.Type == "tier_escalation" {
			escalations++
		}
	}
	if escalations != 2 {
		t.Fatalf("expected 2 tier_escalation alerts, got %d (alerts: %+v)", escalations, alerts)
	}
}

func TestBudgetRouter_AllTiersFail(t *testing.T) {
	t.Parallel()
	cheap, mid, expensive, tiers := newTestTiers()
	cheap.err = errors.New("ollama: connection refused")
	mid.err = errors.New("openai: 500")
	expensive.err = errors.New("anthropic: 503")

	ledger := &mockLedger{spend: 0}
	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100.0,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      2,
	}, ledger)

	req := Request{
		System:    "test",
		Messages:  []Message{{Role: RoleUser, Content: "hello"}},
		MaxTokens: 100,
	}

	_, err := router.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when all tiers fail")
	}
	if !errors.Is(err, cheap.err) {
		t.Fatalf("expected last error to wrap cheapest tier's error, got: %v", err)
	}
}

func TestBudgetRouter_ProviderAndModel(t *testing.T) {
	t.Parallel()
	_, _, _, tiers := newTestTiers()
	ledger := &mockLedger{spend: 0}

	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  100.0,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      2,
	}, ledger)

	if got := router.Provider(); got != "anthropic" {
		t.Fatalf("Provider()=%s, want anthropic", got)
	}
	if got := router.Model(); got != "claude-sonnet-4-6" {
		t.Fatalf("Model()=%s, want claude-sonnet-4-6", got)
	}

	// CurrentTier should reflect the default.
	tier := router.CurrentTier()
	if tier.Name != "large-cloud" {
		t.Fatalf("CurrentTier().Name=%s, want large-cloud", tier.Name)
	}
}

func TestBudgetRouter_MonthlySpentAndRemaining(t *testing.T) {
	t.Parallel()
	_, _, _, tiers := newTestTiers()
	ledger := &mockLedger{spend: 0}

	router := NewBudgetRouter(BudgetConfig{
		MonthlyLimitUSD:  50.0,
		WarnThresholdPct: 0.8,
		TierOrder:        tiers,
		DefaultTier:      1,
	}, ledger)

	// Empty ledger: no spend.
	if spent := router.MonthlySpent(); spent != 0 {
		t.Fatalf("MonthlySpent()=%f, want 0", spent)
	}
	if remaining := router.RemainingBudget(); remaining != 50.0 {
		t.Fatalf("RemainingBudget()=%f, want 50.0", remaining)
	}

	// Simulate spend by updating the mock ledger.
	ledger.setSpend(15.75)

	spent := router.MonthlySpent()
	if spent != 15.75 {
		t.Fatalf("MonthlySpent()=%f, expected 15.75", spent)
	}
	remaining := router.RemainingBudget()
	if remaining != 34.25 {
		t.Fatalf("RemainingBudget()=%f, expected 34.25", remaining)
	}
}
