package governance

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// RateLimitConfig holds the configuration for RateLimitPolicy.
//
// Defaults (applied by NewRateLimitPolicy when the caller passes a zero value):
//   - PerTenantRPS: 100 — 100 requests/second is generous for a clinical workflow
//     but still caps a runaway process or a badly-behaved API client.
//   - PerAgentRPS:  10  — an individual agent rarely needs more than 10 hops/s;
//     a higher rate usually indicates a tight retry loop.
//   - BurstFactor:  3   — allows short bursts (e.g. fan-out to 3 parallel agents)
//     without triggering the limit; keeps the system responsive on startup.
type RateLimitConfig struct {
	// PerTenantRPS is the steady-state rate limit applied across all messages
	// that share the same tenant_id metadata key.
	PerTenantRPS float64

	// PerAgentRPS is the steady-state rate limit applied to messages whose
	// msg.From matches a single agent identity.
	PerAgentRPS float64

	// BurstFactor controls how many tokens above the steady-state rate can
	// accumulate in the bucket. A BurstFactor of 3 and a PerAgentRPS of 10
	// means the bucket can hold up to 30 tokens, allowing short bursts.
	BurstFactor int
}

// defaultConfig is the zero-fill applied in NewRateLimitPolicy when the
// caller supplies a RateLimitConfig with zero-value fields.
var defaultConfig = RateLimitConfig{
	PerTenantRPS: 100,
	PerAgentRPS:  10,
	BurstFactor:  3,
}

// tokenBucket is a simple token-bucket rate limiter.
//
// Tokens are refilled continuously based on elapsed wall-clock time.
// The bucket is not goroutine-safe on its own; callers must hold
// RateLimitPolicy.mu before calling allow().
type tokenBucket struct {
	tokens     float64   // current token count
	maxTokens  float64   // capacity (= rps * burstFactor)
	refillRPS  float64   // tokens added per second
	lastRefill time.Time // wall-clock time of last refill
}

// allow refills the bucket based on elapsed time and then consumes one token.
// Returns true if a token was available (request is allowed), false otherwise.
func (b *tokenBucket) allow() bool {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.lastRefill = now

	// Add tokens proportional to elapsed time, capped at bucket capacity.
	b.tokens += elapsed * b.refillRPS
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// RateLimitPolicy enforces per-tenant and per-agent message rate limits to
// prevent abuse, runaway agents, and cost overruns.
//
// Buckets are created lazily on first use; a new tenant or agent starts with a
// full bucket so it does not suffer a cold-start penalty.
//
// Both the tenant bucket and the agent bucket must have tokens for the message
// to be allowed. If either is empty the message is denied and the reason
// identifies which limit was hit (tenant checked first).
type RateLimitPolicy struct {
	cfg     RateLimitConfig
	tenants map[string]*tokenBucket
	agents  map[string]*tokenBucket
	mu      sync.Mutex
}

// NewRateLimitPolicy constructs a RateLimitPolicy.
//
// Zero-value fields in cfg are replaced by the built-in defaults:
// 100 RPS per tenant, 10 RPS per agent, burst factor 3.
func NewRateLimitPolicy(cfg RateLimitConfig) *RateLimitPolicy {
	if cfg.PerTenantRPS <= 0 {
		cfg.PerTenantRPS = defaultConfig.PerTenantRPS
	}
	if cfg.PerAgentRPS <= 0 {
		cfg.PerAgentRPS = defaultConfig.PerAgentRPS
	}
	if cfg.BurstFactor <= 0 {
		cfg.BurstFactor = defaultConfig.BurstFactor
	}
	return &RateLimitPolicy{
		cfg:     cfg,
		tenants: make(map[string]*tokenBucket),
		agents:  make(map[string]*tokenBucket),
	}
}

// newBucket creates a full token bucket for the given RPS and burst factor.
// Starting full avoids an unfair cold-start penalty for a new tenant or agent.
func (r *RateLimitPolicy) newBucket(rps float64) *tokenBucket {
	max := rps * float64(r.cfg.BurstFactor)
	return &tokenBucket{
		tokens:     max, // start full
		maxTokens:  max,
		refillRPS:  rps,
		lastRefill: time.Now(),
	}
}

// Evaluate applies the per-tenant and per-agent rate limits.
//
// tenant_id is read from msg.Metadata["tenant_id"]. If the key is absent or
// empty the message is bucketed under the sentinel key "" — this means
// unauthenticated traffic shares a single tenant bucket, which is intentionally
// conservative.
//
// The agent identity is msg.From.
//
// Either bucket being empty results in a DecisionDeny with a PHI-free Reason.
// Both conditions are checked every call; the tenant limit is reported first
// when both are exhausted simultaneously.
func (r *RateLimitPolicy) Evaluate(_ context.Context, msg protocol.Message) (PolicyResult, error) {
	// Extract tenant_id from metadata (may be absent → empty string).
	tenantID := ""
	if msg.Metadata != nil {
		if v, ok := msg.Metadata["tenant_id"]; ok {
			if s, ok := v.(string); ok {
				tenantID = s
			}
		}
	}

	agentID := msg.From

	r.mu.Lock()
	defer r.mu.Unlock()

	// Lazy-initialise tenant bucket.
	if _, ok := r.tenants[tenantID]; !ok {
		r.tenants[tenantID] = r.newBucket(r.cfg.PerTenantRPS)
	}
	// Lazy-initialise agent bucket.
	if _, ok := r.agents[agentID]; !ok {
		r.agents[agentID] = r.newBucket(r.cfg.PerAgentRPS)
	}

	tenantOK := r.tenants[tenantID].allow()
	agentOK := r.agents[agentID].allow()

	now := time.Now().UTC()

	if !tenantOK {
		return PolicyResult{
			Decision:  DecisionDeny,
			Reason:    fmt.Sprintf("rate limit exceeded for tenant %s", tenantID),
			CheckedAt: now,
		}, nil
	}
	if !agentOK {
		return PolicyResult{
			Decision:  DecisionDeny,
			Reason:    fmt.Sprintf("rate limit exceeded for agent %s", agentID),
			CheckedAt: now,
		}, nil
	}

	return PolicyResult{
		Decision:  DecisionAllow,
		Reason:    "within rate limit",
		CheckedAt: now,
	}, nil
}
