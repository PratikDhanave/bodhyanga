package engagement

import (
	"testing"
)

// --- Ack: unknown ID ---

func TestAck_UnknownID(t *testing.T) {
	n := NewInMemory()
	_, ok := n.Ack("nonexistent")
	if ok {
		t.Error("expected Ack of unknown ID to return false")
	}
}

// --- AdherenceRate: empty ---

func TestAdherenceRate_Empty(t *testing.T) {
	n := NewInMemory()
	rate := n.AdherenceRate()
	if rate != 0 {
		t.Errorf("AdherenceRate = %f, want 0 for empty notifier", rate)
	}
}

// --- AdherenceRate: partial ack ---

func TestAdherenceRate_PartialAck(t *testing.T) {
	n := NewInMemory()
	n1 := n.Send(Notification{PatientID: "p1", Channel: ChannelSMS, Template: "t", Body: "b"})
	n.Send(Notification{PatientID: "p2", Channel: ChannelEmail, Template: "t", Body: "b"})

	n.Ack(n1.ID)

	rate := n.AdherenceRate()
	if rate != 0.5 {
		t.Errorf("AdherenceRate = %f, want 0.5", rate)
	}
}

// --- AdherenceRate: all acked ---

func TestAdherenceRate_AllAcked(t *testing.T) {
	n := NewInMemory()
	n1 := n.Send(Notification{PatientID: "p1", Channel: ChannelSMS, Template: "t", Body: "b"})
	n2 := n.Send(Notification{PatientID: "p2", Channel: ChannelEmail, Template: "t", Body: "b"})

	n.Ack(n1.ID)
	n.Ack(n2.ID)

	rate := n.AdherenceRate()
	if rate != 1.0 {
		t.Errorf("AdherenceRate = %f, want 1.0", rate)
	}
}

// --- Ack sets AckedAt ---

func TestAck_SetsAckedAt(t *testing.T) {
	n := NewInMemory()
	sent := n.Send(Notification{PatientID: "p1", Channel: ChannelIVR, Template: "t", Body: "b"})
	acked, ok := n.Ack(sent.ID)
	if !ok {
		t.Fatal("Ack returned false for known ID")
	}
	if !acked.Acked {
		t.Error("expected Acked = true")
	}
	if acked.AckedAt.IsZero() {
		t.Error("expected AckedAt to be set")
	}
}

// --- Send with provided ID and SentAt ---

func TestSend_PreservedFields(t *testing.T) {
	n := NewInMemory()
	sent := n.Send(Notification{
		ID:        "custom-id",
		PatientID: "p1",
		Channel:   ChannelSMS,
		Template:  "t",
		Body:      "b",
	})
	if sent.ID != "custom-id" {
		t.Errorf("ID = %q, want %q", sent.ID, "custom-id")
	}
}
