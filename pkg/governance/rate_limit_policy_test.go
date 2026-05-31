package governance

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// msgForTenantAgent builds a minimal protocol.Message with the given
// tenant_id metadata and From field, which are the two keys used by
// RateLimitPolicy for bucket selection.
func msgForTenantAgent(tenantID, agentID string) protocol.Message {
	meta := map[string]any{"tenant_id": tenantID}
	return protocol.NewMessage(agentID, "target", protocol.RoleAgent, "request", "payload", meta)
}

// TestRateLimit_AllowsUnderLimit verifies that a policy configured with a
// generous rate allows a small number of requests without denial.
func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	// 10 RPS per tenant, 10 RPS per agent, burst factor 3 → 30-token bucket
	// Each request consumes one token; 5 requests should all be allowed.
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 10,
		PerAgentRPS:  10,
		BurstFactor:  3,
	})

	msg := msgForTenantAgent("tenant-a", "agent-x")
	for i := 0; i < 5; i++ {
		res, err := p.Evaluate(context.Background(), msg)
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if res.Decision != DecisionAllow {
			t.Fatalf("request %d: expected allow, got %s (reason: %s)", i, res.Decision, res.Reason)
		}
	}
}

// TestRateLimit_DeniesOverLimit verifies that once the bucket is exhausted
// subsequent requests are denied.
func TestRateLimit_DeniesOverLimit(t *testing.T) {
	// 1 RPS, burst factor 1 → 1-token bucket. The very first request is
	// allowed (bucket starts full); the second must be denied.
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 1,
		PerAgentRPS:  1,
		BurstFactor:  1,
	})

	msg := msgForTenantAgent("tenant-b", "agent-y")

	// First request consumes the single token.
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error on first request: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("first request should be allowed, got %s", res.Decision)
	}

	// Second request: bucket is empty → deny.
	res, err = p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error on second request: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("second request should be denied, got %s", res.Decision)
	}
	if res.Reason == "" {
		t.Fatal("deny reason must not be empty")
	}
}

// TestRateLimit_TenantIsolation confirms that exhausting one tenant's bucket
// does not affect a different tenant's bucket.
func TestRateLimit_TenantIsolation(t *testing.T) {
	// 1 RPS per tenant, burst 1 → each tenant has exactly 1 token.
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 1,
		PerAgentRPS:  1000, // high enough that agent bucket never interferes
		BurstFactor:  1,
	})

	tenantA := msgForTenantAgent("tenant-a", "agent-z")
	tenantB := msgForTenantAgent("tenant-b", "agent-z")

	// Drain tenant-a's bucket.
	if res, _ := p.Evaluate(context.Background(), tenantA); res.Decision != DecisionAllow {
		t.Fatalf("tenant-a first request should be allowed, got %s", res.Decision)
	}
	if res, _ := p.Evaluate(context.Background(), tenantA); res.Decision != DecisionDeny {
		t.Fatalf("tenant-a second request should be denied, got %s", res.Decision)
	}

	// tenant-b still has a full bucket — its request must be allowed.
	res, err := p.Evaluate(context.Background(), tenantB)
	if err != nil {
		t.Fatalf("unexpected error for tenant-b: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("tenant-b should be allowed (isolated from tenant-a), got %s (reason: %s)", res.Decision, res.Reason)
	}
}

// TestRateLimit_BurstAllowed verifies that the bucket accumulates burst
// capacity and allows exactly BurstFactor requests before denying.
func TestRateLimit_BurstAllowed(t *testing.T) {
	burst := 5
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 1,
		PerAgentRPS:  1000, // high enough to not interfere
		BurstFactor:  burst,
	})

	// The tenant bucket starts full at 1 * 5 = 5 tokens.
	msg := msgForTenantAgent("tenant-burst", "agent-burst")

	for i := 0; i < burst; i++ {
		res, err := p.Evaluate(context.Background(), msg)
		if err != nil {
			t.Fatalf("burst request %d: unexpected error: %v", i, err)
		}
		if res.Decision != DecisionAllow {
			t.Fatalf("burst request %d: expected allow (bucket has capacity), got %s", i, res.Decision)
		}
	}

	// The (burst+1)-th request must be denied because the bucket is now empty.
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("post-burst request: unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("post-burst request: expected deny, got %s", res.Decision)
	}
}
