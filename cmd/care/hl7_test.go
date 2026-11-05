package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/comm"
	"github.com/PratikDhanave/bodhyanga/pkg/hl7v2"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
	"github.com/PratikDhanave/bodhyanga/pkg/observability"
)

// hl7ADTHandler is the http.HandlerFunc the test mounts on /hl7/adt. It
// mirrors the production handler in cmd/care/main.go; we factor it out as a
// closure here so the integration test stays self-contained and doesn't need
// to spin up the full agent registry.
func hl7ADTHandler(bus comm.Bus, logger observability.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		raw, err := readBody(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/hl7-v2")
		msg, err := hl7v2.ParseMessage(raw)
		if err != nil {
			logger.Printf("hl7 adt parse error: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(hl7v2.BuildACK(hl7v2.MSH{}, "AE", err.Error())))
			return
		}
		ev, err := hl7v2.MessageToADTEvent(msg)
		if err != nil {
			logger.Printf("hl7 adt map error: control_id=%s err=%v", msg.Header.ControlID, err)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(hl7v2.BuildACK(msg.Header, "AE", err.Error())))
			return
		}
		bus.Publish(r.Context(), agent.NewMessage("user", "readmission_tracker",
			agent.RoleUser, "adt_event", medical.EncodeADT(ev),
			map[string]any{"patient_id": ev.PatientID, "hl7_control_id": msg.Header.ControlID}))
		_, _ = w.Write([]byte(hl7v2.BuildACK(msg.Header, "AA", "")))
	}
}

// TestHL7ADTEndpoint_PublishesADTEventAndAcksAA posts the A01 admit fixture,
// asserts the bus receives an adt_event for the right patient, and verifies
// the response body is an AA ACK echoing the original control id.
func TestHL7ADTEndpoint_PublishesADTEventAndAcksAA(t *testing.T) {
	bus := comm.NewInMemoryBus()
	logger := observability.NewStdLogger()

	got := make(chan agent.Message, 4)
	bus.Subscribe("readmission_tracker", func(_ context.Context, m agent.Message) {
		select {
		case got <- m:
		default:
		}
	})

	body, err := os.ReadFile(filepath.Join("..", "..", "pkg", "hl7v2", "testdata", "adt_a01_admit.hl7"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/hl7/adt", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/hl7-v2")
	rec := httptest.NewRecorder()
	hl7ADTHandler(bus, logger)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "MSA|AA|MSG00001") {
		t.Errorf("response missing AA ACK referencing MSG00001: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/hl7-v2" {
		t.Errorf("content-type %q want application/hl7-v2", ct)
	}

	select {
	case m := <-got:
		if m.Type != "adt_event" {
			t.Errorf("bus message type %q want adt_event", m.Type)
		}
		ev, err := medical.DecodeADT(m.Content)
		if err != nil {
			t.Fatalf("decode bus ADTEvent: %v", err)
		}
		if ev.PatientID != "MRN12345" {
			t.Errorf("bus event patient_id %q want MRN12345", ev.PatientID)
		}
		if ev.Type != medical.ADTAdmit {
			t.Errorf("bus event type %q want admit", ev.Type)
		}
		if pid, _ := m.Metadata["patient_id"].(string); pid != "MRN12345" {
			t.Errorf("metadata patient_id %q want MRN12345", pid)
		}
		if cid, _ := m.Metadata["hl7_control_id"].(string); cid != "MSG00001" {
			t.Errorf("metadata hl7_control_id %q want MSG00001", cid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no adt_event published on readmission_tracker topic")
	}
}

// TestHL7ADTEndpoint_TextPlainAlsoAccepted accepts the alternate Content-Type
// many simple curl invocations use.
func TestHL7ADTEndpoint_TextPlainAlsoAccepted(t *testing.T) {
	bus := comm.NewInMemoryBus()
	logger := observability.NewStdLogger()
	var wg sync.WaitGroup
	wg.Add(1)
	bus.Subscribe("readmission_tracker", func(_ context.Context, _ agent.Message) {
		wg.Done()
	})
	body, err := os.ReadFile(filepath.Join("..", "..", "pkg", "hl7v2", "testdata", "adt_a03_discharge.hl7"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/hl7/adt", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	hl7ADTHandler(bus, logger)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("discharge event not published")
	}
}

// TestHL7ADTEndpoint_MalformedReturnsAE400 confirms a missing-MSH body fails
// with HTTP 400 and an AE ACK body, with no panic.
func TestHL7ADTEndpoint_MalformedReturnsAE400(t *testing.T) {
	bus := comm.NewInMemoryBus()
	logger := observability.NewStdLogger()
	body, err := os.ReadFile(filepath.Join("..", "..", "pkg", "hl7v2", "testdata", "adt_malformed.hl7"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/hl7/adt", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/hl7-v2")
	rec := httptest.NewRecorder()
	hl7ADTHandler(bus, logger)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "MSA|AE|") {
		t.Errorf("expected AE ACK on malformed input, got: %s", rec.Body.String())
	}
}
