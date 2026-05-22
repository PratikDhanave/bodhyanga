package engagement

import "testing"

func TestNotifierSendAndAdherence(t *testing.T) {
	n := NewInMemory()
	a := n.Send(Notification{PatientID: "p1", Channel: ChannelSMS, Template: "t", Body: "b"})
	b := n.Send(Notification{PatientID: "p1", Channel: ChannelEmail, Template: "t", Body: "b"})
	if len(n.List()) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(n.List()))
	}
	if _, ok := n.Ack(a.ID); !ok {
		t.Errorf("ack failed for valid id")
	}
	if rate := n.AdherenceRate(); rate != 0.5 {
		t.Errorf("adherence rate: want 0.5, got %v", rate)
	}
	_ = b
}

func TestFormatGapMessage(t *testing.T) {
	got := FormatGapMessage("pt-1", "a1c_overdue", "A1c not measured in 200 days")
	if got == "" || len(got) < 20 {
		t.Errorf("unexpected message: %q", got)
	}
}
