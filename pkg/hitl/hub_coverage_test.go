package hitl

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServeHTTP_MissingTenantID(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	req := httptest.NewRequest("GET", "/ws/hitl", nil)
	rec := httptest.NewRecorder()
	hub.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestServeHTTP_MissingUpgradeHeader(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	req := httptest.NewRequest("GET", "/ws/hitl?tenant_id=t1", nil)
	rec := httptest.NewRecorder()
	hub.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestServeHTTP_MissingWSKey(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	req := httptest.NewRequest("GET", "/ws/hitl?tenant_id=t1", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	rec := httptest.NewRecorder()
	hub.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestServeHTTP_WebSocketUpgrade(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	// Start a real HTTP server.
	srv := httptest.NewServer(hub)
	defer srv.Close()

	// Connect with raw TCP and do the WebSocket handshake.
	addr := strings.TrimPrefix(srv.URL, "http://")
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Send a WebSocket upgrade request.
	reqStr := "GET /ws/hitl?tenant_id=t1 HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"
	fmt.Fprint(conn, reqStr)

	// Read the response.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	rd := bufio.NewReader(conn)
	statusLine, err := rd.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(statusLine, "101") {
		t.Fatalf("expected 101 Switching Protocols, got: %s", statusLine)
	}

	// Read the rest of the headers.
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	// Now broadcast an event — we should receive it as a WebSocket text frame.
	hub.Broadcast("t1", Event{Type: "test_event", At: time.Now().Format(time.RFC3339Nano)})

	// Read the WebSocket frame.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame := make([]byte, 4096)
	n, err := rd.Read(frame)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatal("frame too short")
	}
	// First byte should be 0x81 (FIN + TEXT).
	if frame[0] != 0x81 {
		t.Fatalf("expected 0x81, got 0x%02x", frame[0])
	}

	// Close connection to trigger hub cleanup.
	conn.Close()

	// Give the server goroutine time to clean up.
	time.Sleep(100 * time.Millisecond)
}

func TestServeHTTP_NoHijacker(t *testing.T) {
	q := NewQueue()
	hub := NewHub(q)

	// httptest.ResponseRecorder does not implement http.Hijacker.
	req := httptest.NewRequest("GET", "/ws/hitl?tenant_id=t1", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	rec := httptest.NewRecorder()
	hub.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 (no hijacker), got %d", rec.Code)
	}
}
