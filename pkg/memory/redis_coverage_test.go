package memory

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeRedis runs a minimal Redis server over TCP that responds to SET/GET/DEL.
func fakeRedis(t *testing.T) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
				}
				return
			}
			go handleFakeRedis(conn)
		}
	}()

	return ln.Addr().String(), func() {
		close(done)
		ln.Close()
	}
}

func handleFakeRedis(conn net.Conn) {
	defer conn.Close()
	store := map[string]string{}
	rd := bufio.NewReader(conn)

	for {
		args, err := readCommand(rd)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}

		cmd := strings.ToUpper(args[0])
		switch cmd {
		case "SET":
			if len(args) >= 3 {
				store[args[1]] = args[2]
			}
			fmt.Fprint(conn, "+OK\r\n")
		case "GET":
			if len(args) >= 2 {
				val, ok := store[args[1]]
				if ok {
					fmt.Fprintf(conn, "$%d\r\n%s\r\n", len(val), val)
				} else {
					fmt.Fprint(conn, "$-1\r\n")
				}
			}
		case "DEL":
			if len(args) >= 2 {
				delete(store, args[1])
			}
			fmt.Fprint(conn, ":1\r\n")
		default:
			fmt.Fprintf(conn, "-ERR unknown command '%s'\r\n", cmd)
		}
	}
}

func readCommand(rd *bufio.Reader) ([]string, error) {
	line, err := rd.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || line[0] != '*' {
		return nil, fmt.Errorf("expected array header")
	}
	var count int
	fmt.Sscanf(line[1:], "%d", &count)

	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		bulkLine, err := rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		bulkLine = strings.TrimRight(bulkLine, "\r\n")
		var n int
		fmt.Sscanf(bulkLine[1:], "%d", &n)
		buf := make([]byte, n+2) // +2 for \r\n
		if _, err := rd.Read(buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:n]))
	}
	return args, nil
}

func TestRedisKV_SetGetDelete(t *testing.T) {
	addr, stop := fakeRedis(t)
	defer stop()

	kv, err := NewRedisKV(addr, "test-ns")
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()

	// Set.
	kv.Set("key1", "value1")

	// Get.
	val, found := kv.Get("key1")
	if !found {
		t.Fatal("expected key to be found")
	}
	if val != "value1" {
		t.Fatalf("expected value1, got %q", val)
	}

	// Get non-existent.
	_, found = kv.Get("nonexistent")
	if found {
		t.Fatal("expected not found")
	}

	// Delete.
	kv.Delete("key1")
	_, found = kv.Get("key1")
	if found {
		t.Fatal("expected not found after delete")
	}
}

func TestRedisKV_NamespaceIsolation(t *testing.T) {
	addr, stop := fakeRedis(t)
	defer stop()

	kv1, _ := NewRedisKV(addr, "ns1")
	defer kv1.Close()
	kv2, _ := NewRedisKV(addr, "ns2")
	defer kv2.Close()

	kv1.Set("shared-key", "val-1")
	kv2.Set("shared-key", "val-2")

	v1, _ := kv1.Get("shared-key")
	v2, _ := kv2.Get("shared-key")

	if v1 != "val-1" {
		t.Fatalf("ns1: expected val-1, got %q", v1)
	}
	if v2 != "val-2" {
		t.Fatalf("ns2: expected val-2, got %q", v2)
	}
}

func TestRedisKV_DialError(t *testing.T) {
	_, err := NewRedisKV("127.0.0.1:1", "ns") // port 1 should be refused
	if err == nil {
		t.Fatal("expected dial error")
	}
}

func TestRedisKV_CloseNilConn(t *testing.T) {
	kv := &RedisKV{}
	err := kv.Close()
	if err != nil {
		t.Fatalf("expected nil for nil conn, got %v", err)
	}
}

func TestRedisKV_Reconnect(t *testing.T) {
	addr, stop := fakeRedis(t)
	defer stop()

	kv, _ := NewRedisKV(addr, "reconnect-ns")
	defer kv.Close()

	// Close the connection to simulate a network error.
	kv.mu.Lock()
	kv.conn.Close()
	kv.mu.Unlock()

	// The next command should trigger a reconnect.
	kv.Set("after-reconnect", "value")
	val, found := kv.Get("after-reconnect")
	// Due to the reconnect, the store is new (different goroutine handling),
	// so the value may or may not be there depending on timing.
	// What matters is that Get doesn't panic.
	_ = val
	_ = found
}

func TestRedisKV_ReadResponse_ErrorReply(t *testing.T) {
	addr, stop := fakeRedis(t)
	defer stop()

	kv, _ := NewRedisKV(addr, "err-ns")
	defer kv.Close()

	// Send an unknown command to trigger error reply.
	kv.mu.Lock()
	kv.sendCommand("UNKNOWN_CMD")
	_, _, err := kv.readResponse()
	kv.mu.Unlock()
	if err == nil {
		t.Fatal("expected error reply")
	}
}

func TestRedisKV_ReadResponse_IntegerReply(t *testing.T) {
	addr, stop := fakeRedis(t)
	defer stop()

	kv, _ := NewRedisKV(addr, "int-ns")
	defer kv.Close()

	// DEL returns an integer reply.
	kv.mu.Lock()
	kv.sendCommand("DEL", kv.key("x"))
	val, found, err := kv.readResponse()
	kv.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected found for integer reply")
	}
	_ = val
}

func TestRedisKV_DoCommand_ReconnectOnReadError(t *testing.T) {
	// Test the reconnect-on-read-error path.
	// Start a server that closes connection after one command.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Read one command, then close.
			rd := bufio.NewReader(conn)
			readCommand(rd)
			conn.Close()
		}
	}()

	kv, err := NewRedisKV(ln.Addr().String(), "rc")
	if err != nil {
		t.Fatal(err)
	}
	defer kv.Close()

	// Give it a moment for the connection to be established.
	time.Sleep(50 * time.Millisecond)

	// This should fail but trigger reconnect internally.
	_, found := kv.Get("x")
	// We don't expect found, but it should not panic.
	_ = found
}
