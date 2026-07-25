package memory

// Package memory — redis.go
//
// # Redis backend for KeyValueStore using RESP2 over raw TCP
//
// This file implements a Redis-backed KeyValueStore without any external
// dependencies. It speaks the Redis Serialization Protocol version 2 (RESP2)
// directly over a net.Conn, which is exactly what a production client library
// such as go-redis does underneath.
//
// # RESP2 primer
//
// Every message in RESP2 starts with a single byte that identifies its type:
//
//	'+' — Simple String  — e.g. "+OK\r\n"
//	'-' — Error          — e.g. "-ERR unknown command\r\n"
//	':' — Integer        — e.g. ":1\r\n"
//	'$' — Bulk String    — e.g. "$5\r\nhello\r\n"   (length-prefixed)
//	                        or "$-1\r\n"             (null bulk string)
//	'*' — Array          — e.g. "*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n"
//
// Commands are sent as RESP2 Arrays where every element is a Bulk String.
// That encoding is called the "inline array" or "multi-bulk command" format.
//
// Example: SET foo bar
//
//	*3\r\n          ← array of 3 elements
//	$3\r\nSET\r\n   ← first element: 3-byte bulk string "SET"
//	$3\r\nfoo\r\n   ← second element: "foo"
//	$3\r\nbar\r\n   ← third element: "bar"
//
// Redis replies:
//
//	+OK\r\n

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
)

// RedisKV is a KeyValueStore backed by a single Redis TCP connection.
//
// It is intentionally minimal — no connection pooling, no pipelining, no
// cluster support. It is meant to demonstrate how the RESP2 protocol works at
// the wire level and to satisfy the KeyValueStore interface in educational
// scenarios.
//
// Thread-safety: a sync.Mutex serialises every command so that concurrent
// callers do not interleave their reads and writes on the shared net.Conn.
//
// Reconnect: if a command fails due to a network error the client makes one
// reconnect attempt before surfacing the error to the caller.
type RedisKV struct {
	addr string      // TCP address: "host:port"
	ns   string      // namespace prefix applied to every key
	conn net.Conn    // live TCP connection to Redis
	rd   *bufio.Reader
	mu   sync.Mutex  // serialises all network I/O
}

// NewRedisKV dials a Redis server over TCP and returns a RedisKV.
//
// addr is the TCP address of the Redis server (e.g. "127.0.0.1:6379").
// namespace is prepended to every key as "<namespace>:<key>" so that multiple
// stores can share the same Redis instance without key collisions.
//
// Example:
//
//	kv, err := NewRedisKV("127.0.0.1:6379", "agent-1")
func NewRedisKV(addr, namespace string) (*RedisKV, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("redis: dial %s: %w", addr, err)
	}
	return &RedisKV{
		addr: addr,
		ns:   namespace,
		conn: conn,
		rd:   bufio.NewReader(conn),
	}, nil
}

// Close shuts down the underlying TCP connection.
func (r *RedisKV) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

// key returns the namespaced key: "<namespace>:<k>".
// Using a namespace means multiple RedisKV instances (or multiple agents) can
// share the same Redis server without stomping on each other's data.
func (r *RedisKV) key(k string) string {
	return r.ns + ":" + k
}

// Get fetches the value for key. Returns ("", false) when the key does not
// exist (Redis nil bulk string reply).
//
// Under the hood it sends:
//
//	GET <namespace>:<key>
//
// and parses the $N / $-1 bulk string reply.
func (r *RedisKV) Get(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	val, found, err := r.doCommand(false, "GET", r.key(key))
	if err != nil {
		return "", false
	}
	return val, found
}

// Set stores value under key. It panics on neither value nor error (which
// cannot happen in normal operation).
//
// Under the hood it sends:
//
//	SET <namespace>:<key> <value>
//
// and expects a "+OK\r\n" simple-string reply.
func (r *RedisKV) Set(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, _, _ = r.doCommand(false, "SET", r.key(key), value)
}

// Delete removes the key. It is a no-op when the key does not exist.
//
// Under the hood it sends:
//
//	DEL <namespace>:<key>
//
// and expects an ":N\r\n" integer reply indicating how many keys were removed.
func (r *RedisKV) Delete(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, _, _ = r.doCommand(false, "DEL", r.key(key))
}

// doCommand sends a RESP2 command and reads one response. If the first attempt
// fails with a network error the function reconnects (once) and retries.
//
// isRetry must be false on the first call; the function sets it to true
// internally for the single retry.
//
// Returns:
//
//	value  — the string payload (empty for integer / +OK replies)
//	found  — true unless Redis returned a null bulk string ($-1)
//	err    — non-nil on protocol or transport errors
func (r *RedisKV) doCommand(isRetry bool, args ...string) (string, bool, error) {
	if err := r.sendCommand(args...); err != nil {
		if !isRetry {
			if rerr := r.reconnect(); rerr == nil {
				return r.doCommand(true, args...)
			}
		}
		return "", false, err
	}

	val, found, err := r.readResponse()
	if err != nil {
		if !isRetry {
			if rerr := r.reconnect(); rerr == nil {
				return r.doCommand(true, args...)
			}
		}
		return "", false, err
	}
	return val, found, nil
}

// reconnect closes the current connection (if open) and dials a new one.
// It replaces r.conn and r.rd so subsequent commands use the fresh connection.
func (r *RedisKV) reconnect() error {
	if r.conn != nil {
		_ = r.conn.Close()
	}
	conn, err := net.Dial("tcp", r.addr)
	if err != nil {
		return fmt.Errorf("redis: reconnect %s: %w", r.addr, err)
	}
	r.conn = conn
	r.rd = bufio.NewReader(conn)
	return nil
}

// sendCommand encodes args as a RESP2 Array of Bulk Strings and writes it to
// the connection.
//
// Wire format for ["SET", "k", "v"]:
//
//	*3\r\n
//	$3\r\nSET\r\n
//	$1\r\nk\r\n
//	$1\r\nv\r\n
func (r *RedisKV) sendCommand(args ...string) error {
	var b strings.Builder
	// Array header
	b.WriteString(fmt.Sprintf("*%d\r\n", len(args)))
	// Each element as a Bulk String
	for _, arg := range args {
		b.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg))
	}
	_, err := fmt.Fprint(r.conn, b.String())
	return err
}

// readResponse reads exactly one RESP2 response from the buffered reader.
//
// The type byte determines how the rest of the line (and possibly additional
// data) is parsed:
//
//	'+' Simple String  → returns (line, true, nil)
//	'-' Error          → returns ("", false, error)
//	':' Integer        → returns (intStr, true, nil)
//	'$' Bulk String    → reads length; if -1 returns ("", false, nil) (nil/missing);
//	                     otherwise reads exactly N bytes + trailing \r\n
//
// Any unrecognised type byte is returned as an error.
func (r *RedisKV) readResponse() (string, bool, error) {
	// ReadString reads up to and including the delimiter.
	line, err := r.rd.ReadString('\n')
	if err != nil {
		return "", false, fmt.Errorf("redis: read line: %w", err)
	}
	// Strip trailing \r\n
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return "", false, fmt.Errorf("redis: empty response line")
	}

	typeChar := line[0]
	rest := line[1:]

	switch typeChar {
	case '+':
		// Simple String: "+OK"
		return rest, true, nil

	case '-':
		// Error: "-ERR …"
		return "", false, fmt.Errorf("redis: server error: %s", rest)

	case ':':
		// Integer: ":0" or ":1"
		return rest, true, nil

	case '$':
		// Bulk String: "$-1" (null) or "$N"
		n, err := strconv.Atoi(rest)
		if err != nil {
			return "", false, fmt.Errorf("redis: invalid bulk length %q: %w", rest, err)
		}
		if n == -1 {
			// Null bulk string — key does not exist.
			return "", false, nil
		}
		// Read exactly n bytes of payload plus the trailing \r\n (2 bytes).
		buf := make([]byte, n+2)
		// bufio.Reader.Read may return fewer than len(buf) bytes from a single
		// underlying read; use io.ReadFull to read the whole payload (+CRLF),
		// otherwise large values are silently truncated and the stream is left
		// mis-aligned for the next reply.
		if _, err := io.ReadFull(r.rd, buf); err != nil {
			return "", false, fmt.Errorf("redis: read bulk data: %w", err)
		}
		// Payload is everything except the trailing \r\n.
		return string(buf[:n]), true, nil

	default:
		return "", false, fmt.Errorf("redis: unexpected type byte %q in %q", typeChar, line)
	}
}
