// nats_stub.go — placeholder used when the `nats` build tag is NOT set.
//
// Bodhyanga's default build does not link the NATS client. This stub exports
// NewNATSBus so callers can reference it unconditionally; the function
// returns a descriptive error rather than a usable bus. The real
// implementation lives in nats.go behind `//go:build nats`.
//
// To get a real NATS JetStream bus:
//
//	go build -tags nats ./cmd/care

//go:build !nats

package comm

import (
	"context"
	"errors"

	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// NATSBus is a placeholder type in the default build. It carries no state
// and every method is a no-op. The type exists only so that callers that
// reference NATSBus do not require a separate build-tag guard. It
// implements comm.Bus so that type-checked code (e.g. selectBus in
// cmd/care/main.go) compiles without conditional compilation on every call
// site — but NewNATSBus always returns (nil, err), so a real *NATSBus
// value is never reachable in this build.
type NATSBus struct{}

// NewNATSBus always returns an error in the default build. The caller
// should fall back to NewInMemoryBus when this error is returned.
//
//	bus, err := comm.NewNATSBus()
//	if err != nil {
//	    log.Printf("[care] NATS unavailable (%v), using in-memory bus", err)
//	    bus = comm.NewInMemoryBus()
//	}
func NewNATSBus() (*NATSBus, error) {
	return nil, errors.New("comm: NATS bus requires the 'nats' build tag; rebuild with -tags nats")
}

// Subscribe is a no-op stub. It will never be called because NewNATSBus
// always returns an error in the !nats build; the method exists only to
// satisfy the Bus interface so callers compile without build-tag guards.
func (b *NATSBus) Subscribe(_ string, _ Handler) func() { return func() {} }

// Publish is a no-op stub. It will never be called because NewNATSBus
// always returns an error in the !nats build; the method exists only to
// satisfy the Bus interface so callers compile without build-tag guards.
func (b *NATSBus) Publish(_ context.Context, _ protocol.Message) {}

// Compile-time assertion: NATSBus satisfies Bus in both builds.
var _ Bus = (*NATSBus)(nil)
