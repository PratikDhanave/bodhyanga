package llm

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SpendLedger is the subset of analytics.CostLedger that BudgetRouter needs.
// Defining it here avoids a circular import (pkg/llm -> pkg/analytics ->
// pkg/eval -> pkg/llm). Callers pass their *analytics.CostLedger which
// satisfies this interface.
type SpendLedger interface {
	// MonthlySpendUSD returns the total USD spent in [from, to] across all
	// tenants. Implementations should match the semantics of
	// analytics.CostLedger.Summarize(from, to, "").TotalCostUSD.
	MonthlySpendUSD(from, to time.Time) float64

	// RecordUsage records token usage for cost accounting.
	RecordUsage(provider, model string, inputTokens, outputTokens, cacheTokens int)
}

// BudgetConfig controls how the BudgetRouter selects LLM tiers based on
// cumulative monthly spend.
type BudgetConfig struct {
	// MonthlyLimitUSD is the hard monthly cap in USD. Once spend reaches this
	// amount, all requests are routed to the cheapest tier (index 0).
	MonthlyLimitUSD float64

	// WarnThresholdPct is the fraction of MonthlyLimitUSD at which a warning
	// alert fires and routing falls back to the cheapest tier.
	// For example, 0.8 means 80%.
	WarnThresholdPct float64

	// TierOrder lists available tiers from cheapest (index 0) to most
	// expensive. The router tries cheaper tiers first when budget is
	// constrained.
	TierOrder []TierConfig

	// DefaultTier is the index into TierOrder used when spend is within
	// budget. Typically points to a higher-quality (more expensive) tier.
	DefaultTier int
}

// TierConfig describes one LLM tier that the BudgetRouter can route to.
type TierConfig struct {
	Name     string // human-readable label: "local", "small-cloud", "large-cloud"
	Provider string // "ollama", "openai", "anthropic"
	Model    string // "llama3.2", "gpt-4o-mini", "claude-sonnet-4-6"
	Client   Client // the underlying LLM client for this tier
}

// BudgetAlert is emitted when the BudgetRouter detects a budget threshold
// has been crossed or a tier escalation/fallback occurs.
type BudgetAlert struct {
	Type      string // "warning" | "hard_limit" | "tier_escalation"
	Message   string
	SpentUSD  float64
	LimitUSD  float64
	Timestamp time.Time
}

// BudgetRouter implements Client and routes LLM requests to the cheapest
// viable tier based on remaining monthly budget. When spend exceeds the
// configured thresholds it degrades to cheaper tiers and emits alerts.
type BudgetRouter struct {
	cfg     BudgetConfig
	ledger  SpendLedger
	mu      sync.RWMutex
	alerts  []BudgetAlert
	onAlert func(BudgetAlert) // callback for notifications
}

// NewBudgetRouter constructs a BudgetRouter that reads spend from ledger
// and routes according to cfg.
func NewBudgetRouter(cfg BudgetConfig, ledger SpendLedger) *BudgetRouter {
	return &BudgetRouter{
		cfg:    cfg,
		ledger: ledger,
	}
}

// SetAlertCallback registers fn to be called whenever a BudgetAlert is
// emitted. The callback is invoked synchronously under the router's lock,
// so it should be fast (e.g. enqueue to a channel).
func (r *BudgetRouter) SetAlertCallback(fn func(BudgetAlert)) {
	r.mu.Lock()
	r.onAlert = fn
	r.mu.Unlock()
}

// Provider returns the provider string for the currently selected tier.
func (r *BudgetRouter) Provider() string {
	return r.CurrentTier().Provider
}

// Model returns the model string for the currently selected tier.
func (r *BudgetRouter) Model() string {
	return r.CurrentTier().Model
}

// Complete implements Client. It selects a tier based on monthly spend,
// forwards the request, records cost, and cascades to cheaper tiers on
// failure.
func (r *BudgetRouter) Complete(ctx context.Context, req Request) (*Response, error) {
	spent := r.MonthlySpent()
	tierIdx := r.selectTier(spent)

	// Try the selected tier, then cascade down to cheaper tiers on failure.
	var lastErr error
	for i := tierIdx; i >= 0; i-- {
		tier := r.cfg.TierOrder[i]

		resp, err := tier.Client.Complete(ctx, req)
		if err != nil {
			lastErr = err
			// Emit a tier_escalation alert when falling back.
			if i > 0 {
				r.emitAlert(BudgetAlert{
					Type:      "tier_escalation",
					Message:   fmt.Sprintf("tier %q failed (%v), falling back to %q", tier.Name, err, r.cfg.TierOrder[i-1].Name),
					SpentUSD:  spent,
					LimitUSD:  r.cfg.MonthlyLimitUSD,
					Timestamp: time.Now(),
				})
			}
			continue
		}

		// Record cost in the ledger.
		r.ledger.RecordUsage(tier.Provider, tier.Model,
			resp.InputTokens, resp.OutputTokens, resp.CachedTokens)

		return resp, nil
	}

	return nil, fmt.Errorf("budget: all tiers failed, last error: %w", lastErr)
}

// selectTier decides which tier index to use based on current spend.
// It also emits alerts when thresholds are crossed.
func (r *BudgetRouter) selectTier(spent float64) int {
	limit := r.cfg.MonthlyLimitUSD

	// Hard limit: at or over 100%.
	if limit > 0 && spent >= limit {
		r.emitAlert(BudgetAlert{
			Type:      "hard_limit",
			Message:   fmt.Sprintf("monthly spend $%.2f has reached limit $%.2f — routing to cheapest tier", spent, limit),
			SpentUSD:  spent,
			LimitUSD:  limit,
			Timestamp: time.Now(),
		})
		return 0
	}

	// Warning threshold.
	if limit > 0 && r.cfg.WarnThresholdPct > 0 && spent >= limit*r.cfg.WarnThresholdPct {
		r.emitAlert(BudgetAlert{
			Type:      "warning",
			Message:   fmt.Sprintf("monthly spend $%.2f has reached %.0f%% of limit $%.2f — routing to cheapest tier", spent, r.cfg.WarnThresholdPct*100, limit),
			SpentUSD:  spent,
			LimitUSD:  limit,
			Timestamp: time.Now(),
		})
		return 0
	}

	// Under budget: use default tier.
	idx := r.cfg.DefaultTier
	if idx < 0 || idx >= len(r.cfg.TierOrder) {
		return 0
	}
	return idx
}

// emitAlert records the alert and fires the callback if set.
func (r *BudgetRouter) emitAlert(a BudgetAlert) {
	r.mu.Lock()
	r.alerts = append(r.alerts, a)
	fn := r.onAlert
	r.mu.Unlock()
	if fn != nil {
		fn(a)
	}
}

// CurrentTier returns the TierConfig that would be selected for the next
// request given current spend.
func (r *BudgetRouter) CurrentTier() TierConfig {
	spent := r.MonthlySpent()
	idx := r.selectTierQuiet(spent)
	return r.cfg.TierOrder[idx]
}

// selectTierQuiet is like selectTier but does not emit alerts. Used by
// read-only accessors (CurrentTier, Provider, Model) to avoid side effects.
func (r *BudgetRouter) selectTierQuiet(spent float64) int {
	limit := r.cfg.MonthlyLimitUSD
	if limit > 0 && spent >= limit {
		return 0
	}
	if limit > 0 && r.cfg.WarnThresholdPct > 0 && spent >= limit*r.cfg.WarnThresholdPct {
		return 0
	}
	idx := r.cfg.DefaultTier
	if idx < 0 || idx >= len(r.cfg.TierOrder) {
		return 0
	}
	return idx
}

// MonthlySpent returns the total USD spent in the current calendar month
// across all tenants.
func (r *BudgetRouter) MonthlySpent() float64 {
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	return r.ledger.MonthlySpendUSD(monthStart, monthEnd)
}

// Alerts returns a copy of all alerts emitted so far.
func (r *BudgetRouter) Alerts() []BudgetAlert {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BudgetAlert, len(r.alerts))
	copy(out, r.alerts)
	return out
}

// RemainingBudget returns MonthlyLimitUSD minus current monthly spend. If
// spend exceeds the limit the result is negative.
func (r *BudgetRouter) RemainingBudget() float64 {
	return r.cfg.MonthlyLimitUSD - r.MonthlySpent()
}
