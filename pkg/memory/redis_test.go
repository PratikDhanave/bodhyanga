package memory

// redis_test.go — tests for RedisKV using a mock TCP server.
//
// No real Redis is required. A minimal mock server is started in-process via
// net.Listen. It parses incoming RESP2 Array commands, operates on an
// in-memory map, and replies with valid RESP2 responses.
//
// This lets us:
//   - test the full RESP2 encoding/decoding path
//   - verify namespace prefixing
//   - run quickly and deterministically in CI

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ─────────────────────────────────────────────
// Mock Redis server
// ─────────────────────────────────────────────

// mockRedis is a tiny in-memory server that speaks RESP2 and understands
// GET, SET, and DEL. It is deliberately simple — just enough for the tests.
type mockRedis struct {
	ln   net.Listener
	mu   sync.Mutex
	data map[string]string
	addr string
}

// newMockRedis starts a mock Redis server on a random localhost port.
func newMockRedis(t *testing.T) *mockRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mock redis: listen: %v", err)
	}
	m := &mockRedis{
		ln:   ln,
		data: make(map[string]string),
		addr: ln.Addr().String(),
	}
	go m.acceptLoop()
	return m
}

// close shuts down the mock server.
func (m *mockRedis) close() { _ = m.ln.Close() }

// acceptLoop handles one connection at a time (sufficient for unit tests).
func (m *mockRedis) acceptLoop() {
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			return // listener closed
		}
		go m.handleConn(conn)
	}
}

// handleConn reads RESP2 commands and writes RESP2 replies until the
// connection is closed.
func (m *mockRedis) handleConn(conn net.Conn) {
	defer conn.Close()
	rd := bufio.NewReader(conn)

	for {
		args, err := readRESP2Command(rd)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}

		cmd := strings.ToUpper(args[0])

		switch cmd {
		case "GET":
			if len(args) < 2 {
				_, _ = fmt.Fprintf(conn, "-ERR wrong number of arguments for GET\r\n")
				continue
			}
			m.mu.Lock()
			val, ok := m.data[args[1]]
			m.mu.Unlock()
			if !ok {
				_, _ = fmt.Fprintf(conn, "$-1\r\n")
			} else {
				_, _ = fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(val), val)
			}

		case "SET":
			if len(args) < 3 {
				_, _ = fmt.Fprintf(conn, "-ERR wrong number of arguments for SET\r\n")
				continue
			}
			m.mu.Lock()
			m.data[args[1]] = args[2]
			m.mu.Unlock()
			_, _ = fmt.Fprintf(conn, "+OK\r\n")

		case "DEL":
			if len(args) < 2 {
				_, _ = fmt.Fprintf(conn, "-ERR wrong number of arguments for DEL\r\n")
				continue
			}
			m.mu.Lock()
			_, existed := m.data[args[1]]
			delete(m.data, args[1])
			m.mu.Unlock()
			if existed {
				_, _ = fmt.Fprintf(conn, ":1\r\n")
			} else {
				_, _ = fmt.Fprintf(conn, ":0\r\n")
			}

		default:
			_, _ = fmt.Fprintf(conn, "-ERR unknown command '%s'\r\n", cmd)
		}
	}
}

// readRESP2Command reads one complete RESP2 Array command from rd.
// It returns the slice of string arguments.
func readRESP2Command(rd *bufio.Reader) ([]string, error) {
	// Read the array header: "*N\r\n"
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] != '*' {
		return nil, fmt.Errorf("expected array, got %q", line)
	}
	count, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, fmt.Errorf("bad array count %q: %w", line, err)
	}

	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		// Read bulk string header: "$N\r\n"
		header, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		header = strings.TrimRight(header, "\r\n")
		if len(header) == 0 || header[0] != '$' {
			return nil, fmt.Errorf("expected bulk string header, got %q", header)
		}
		n, err := strconv.Atoi(header[1:])
		if err != nil {
			return nil, fmt.Errorf("bad bulk length %q: %w", header, err)
		}
		// Read N bytes + \r\n
		buf := make([]byte, n+2)
		if _, err := rd.Read(buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:n]))
	}
	return args, nil
}

// ─────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────

// TestRedisKV_SetGet verifies that a value written with Set is retrievable
// with Get.
func TestRedisKV_SetGet(t *testing.T) {
	srv := newMockRedis(t)
	defer srv.close()

	kv, err := NewRedisKV(srv.addr, "test")
	if err != nil {
		t.Fatalf("NewRedisKV: %v", err)
	}
	defer kv.Close()

	kv.Set("color", "blue")

	got, ok := kv.Get("color")
	if !ok {
		t.Fatal("Get: expected key to exist")
	}
	if got != "blue" {
		t.Fatalf("Get: got %q, want %q", got, "blue")
	}
}

// TestRedisKV_GetMissing verifies that Get returns ("", false) for a key that
// was never stored. Redis returns the null bulk string "$-1\r\n" in this case.
func TestRedisKV_GetMissing(t *testing.T) {
	srv := newMockRedis(t)
	defer srv.close()

	kv, err := NewRedisKV(srv.addr, "test")
	if err != nil {
		t.Fatalf("NewRedisKV: %v", err)
	}
	defer kv.Close()

	got, ok := kv.Get("does-not-exist")
	if ok {
		t.Fatalf("Get: expected ok=false for missing key, got %q", got)
	}
	if got != "" {
		t.Fatalf("Get: expected empty string for missing key, got %q", got)
	}
}

// TestRedisKV_Delete verifies that Delete removes a key so subsequent Gets
// return ("", false).
func TestRedisKV_Delete(t *testing.T) {
	srv := newMockRedis(t)
	defer srv.close()

	kv, err := NewRedisKV(srv.addr, "test")
	if err != nil {
		t.Fatalf("NewRedisKV: %v", err)
	}
	defer kv.Close()

	kv.Set("animal", "cat")
	if _, ok := kv.Get("animal"); !ok {
		t.Fatal("setup: expected key to exist before delete")
	}

	kv.Delete("animal")

	if got, ok := kv.Get("animal"); ok {
		t.Fatalf("Get: expected key absent after delete, got %q", got)
	}
}

// TestRedisKV_DeleteMissing verifies that deleting a non-existent key is a
// safe no-op (Redis replies ":0\r\n", which we silently ignore).
func TestRedisKV_DeleteMissing(t *testing.T) {
	srv := newMockRedis(t)
	defer srv.close()

	kv, err := NewRedisKV(srv.addr, "test")
	if err != nil {
		t.Fatalf("NewRedisKV: %v", err)
	}
	defer kv.Close()

	// Must not panic or error.
	kv.Delete("ghost-key")
}

// TestRedisKV_Namespace verifies that the namespace prefix is applied so that
// two RedisKV instances with different namespaces do not share keys.
func TestRedisKV_Namespace(t *testing.T) {
	srv := newMockRedis(t)
	defer srv.close()

	a, err := NewRedisKV(srv.addr, "ns-a")
	if err != nil {
		t.Fatalf("NewRedisKV ns-a: %v", err)
	}
	defer a.Close()

	b, err := NewRedisKV(srv.addr, "ns-b")
	if err != nil {
		t.Fatalf("NewRedisKV ns-b: %v", err)
	}
	defer b.Close()

	a.Set("key", "from-a")
	b.Set("key", "from-b")

	gotA, okA := a.Get("key")
	if !okA || gotA != "from-a" {
		t.Fatalf("ns-a Get: got (%q, %v), want (\"from-a\", true)", gotA, okA)
	}

	gotB, okB := b.Get("key")
	if !okB || gotB != "from-b" {
		t.Fatalf("ns-b Get: got (%q, %v), want (\"from-b\", true)", gotB, okB)
	}
}

// TestRedisKV_Overwrite verifies that calling Set twice on the same key
// updates the value.
func TestRedisKV_Overwrite(t *testing.T) {
	srv := newMockRedis(t)
	defer srv.close()

	kv, err := NewRedisKV(srv.addr, "test")
	if err != nil {
		t.Fatalf("NewRedisKV: %v", err)
	}
	defer kv.Close()

	kv.Set("x", "first")
	kv.Set("x", "second")

	got, ok := kv.Get("x")
	if !ok {
		t.Fatal("Get: expected key to exist after overwrite")
	}
	if got != "second" {
		t.Fatalf("Get: got %q, want %q", got, "second")
	}
}

// TestRedisKV_ImplementsInterface is a compile-time assertion that *RedisKV
// satisfies the KeyValueStore interface.
func TestRedisKV_ImplementsInterface(t *testing.T) {
	var _ KeyValueStore = (*RedisKV)(nil)
}

// TestRedisKV_KeyHelper verifies the key() helper appends the namespace prefix
// correctly.
func TestRedisKV_KeyHelper(t *testing.T) {
	r := &RedisKV{ns: "myns"}
	got := r.key("foo")
	want := "myns:foo"
	if got != want {
		t.Fatalf("key(): got %q, want %q", got, want)
	}
}
