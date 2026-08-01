package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/PratikDhanave/bodh/pkg/comm"
	"github.com/PratikDhanave/bodh/pkg/hitl"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// mirrorBusToHub is the backend half of the event-driven SPA. It subscribes to
// every message on the bus and re-emits it to the WebSocket hub as a thin
// envelope — the message Type as the event kind, plus the correlating
// identifiers. The SPA subscribes to the kinds it cares about and re-reads the
// authoritative endpoint on each event, so pages update in real time without
// polling or a manual reload.
//
// PHI stays off the socket: only case_id / patient_id / from / to travel — never
// the clinical content — mirroring the audit log's "enough to correlate, never
// enough to leak" stance.
func mirrorBusToHub(bus *comm.InMemoryBus, hub *hitl.Hub, tenant string) {
	bus.Subscribe("", func(_ context.Context, msg protocol.Message) {
		if hub == nil {
			return
		}
		caseID, _ := msg.Metadata["case_id"].(string)
		patientID, _ := msg.Metadata["patient_id"].(string)
		payload, _ := json.Marshal(map[string]string{
			"case_id":    caseID,
			"patient_id": patientID,
			"from":       msg.From,
			"to":         msg.To,
		})
		hub.Broadcast(tenant, hitl.Event{
			Type:    msg.Type,
			Payload: json.RawMessage(payload),
			At:      time.Now().UTC().Format(time.RFC3339Nano),
		})
	})
}
