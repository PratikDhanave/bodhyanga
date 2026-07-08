//go:build !nats

package comm

// This test asserts the NATS STUB contract: when the real JetStream client is
// not linked (the default build), NewNATSBus must return an error and the
// zero-value bus's methods must be safe no-ops. It is constrained to !nats
// because under -tags nats the real implementation links, NewNATSBus actually
// dials a server, and the zero-value struct is no longer a no-op stub — the
// real behaviour is covered by nats_integration_test.go instead.

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

func TestNATSBus_Stub(t *testing.T) {
	bus, err := NewNATSBus()
	if err == nil {
		t.Error("expected error from NATS stub")
	}
	if bus != nil {
		t.Error("expected nil bus from NATS stub")
	}

	// Exercise the stub methods to get coverage (they are no-ops).
	var stub NATSBus
	unsub := stub.Subscribe("agent1", func(_ context.Context, _ protocol.Message) {})
	if unsub == nil {
		t.Error("Subscribe should return non-nil unsubscribe")
	}
	unsub() // Should not panic.

	stub.Publish(context.Background(), protocol.Message{}) // Should not panic.
}
