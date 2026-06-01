// Command audit-verify verifies the integrity of a Bodh immutable audit log.
//
// Usage:
//
//	go run ./cmd/audit-verify -file /var/lib/bodh/audit.jsonl
//
// Exit codes:
//
//	0 — chain is intact
//	1 — chain is broken (tampering detected) or file is unreadable
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/PratikDhanave/bodh/pkg/audit"
)

func main() {
	filePath := flag.String("file", "", "path to the JSONL audit log file (required)")
	flag.Parse()

	if *filePath == "" {
		fmt.Fprintln(os.Stderr, "audit-verify: -file is required")
		flag.Usage()
		os.Exit(1)
	}

	// Collect stats by scanning the file.
	stats, err := collectStats(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit-verify: %v\n", err)
		os.Exit(1)
	}

	// Print header.
	fmt.Printf("Audit log: %s\n", *filePath)
	fmt.Printf("Chain length: %d events\n", stats.count)
	if stats.count > 0 {
		fmt.Printf("First event: seq=%d  id=%s  at=%s  kind=%s\n",
			stats.firstSeq, stats.firstID, stats.firstAt.Format(time.RFC3339), stats.firstKind)
		fmt.Printf("Last event:  seq=%d  id=%s  at=%s  kind=%s\n",
			stats.lastSeq, stats.lastID, stats.lastAt.Format(time.RFC3339), stats.lastKind)
	}

	// Verify the chain using a throw-away ImmutableRecorder (we only need VerifyChain).
	// We can construct one without opening the file for writing by using a temp file.
	tmpF, err := os.CreateTemp("", "audit-verify-probe-*.jsonl")
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit-verify: create temp: %v\n", err)
		os.Exit(1)
	}
	tmpPath := tmpF.Name()
	tmpF.Close()
	defer os.Remove(tmpPath)

	rec, err := audit.NewImmutableRecorder(tmpPath, nil, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit-verify: init verifier: %v\n", err)
		os.Exit(1)
	}
	defer rec.Close()

	ok, brokenAt, err := rec.VerifyChain(*filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit-verify: chain error: %v\n", err)
		os.Exit(1)
	}

	if ok {
		fmt.Println("Tamper status: INTACT — all hashes verified")
		os.Exit(0)
	}

	fmt.Printf("Tamper status: BROKEN at seq=%d\n", brokenAt)
	fmt.Println("WARNING: the audit log has been tampered with.")
	os.Exit(1)
}

// chainedEvent mirrors audit.ChainedEvent for local parsing (avoids exporting
// the struct fields we only need here).
type chainedEvent struct {
	Event    struct {
		ID   string    `json:"id"`
		At   time.Time `json:"at"`
		Kind string    `json:"kind"`
	} `json:"event"`
	Seq int64 `json:"seq"`
}

type fileStats struct {
	count     int64
	firstSeq  int64
	firstID   string
	firstAt   time.Time
	firstKind string
	lastSeq   int64
	lastID    string
	lastAt    time.Time
	lastKind  string
}

func collectStats(filePath string) (*fileStats, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	stats := &fileStats{}
	var first bool = true

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ce chainedEvent
		if err := json.Unmarshal(line, &ce); err != nil {
			return nil, fmt.Errorf("parse error: %w", err)
		}
		stats.count++
		if first {
			stats.firstSeq = ce.Seq
			stats.firstID = ce.Event.ID
			stats.firstAt = ce.Event.At
			stats.firstKind = ce.Event.Kind
			first = false
		}
		stats.lastSeq = ce.Seq
		stats.lastID = ce.Event.ID
		stats.lastAt = ce.Event.At
		stats.lastKind = ce.Event.Kind
	}
	return stats, scanner.Err()
}
