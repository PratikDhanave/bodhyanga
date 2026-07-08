package audit

// immutable.go — tamper-evident, append-only audit sink with Merkle chain-hash.
//
// Design goals
//   - Every ChainedEvent carries SHA-256(prev_hash + json(event)). Any
//     modification to a historical line breaks the hash of every subsequent
//     line, making tampering immediately detectable via VerifyChain.
//   - The file is opened O_APPEND|O_CREATE so writes are atomic at the OS
//     level (POSIX guarantee for writes ≤ PIPE_BUF); the mu lock provides
//     additional safety for concurrent goroutines.
//   - S3 upload is fire-and-forget so it cannot block the clinical hot path.
//
// HIPAA relevance: §164.312(b) requires audit controls; the Merkle chain
// satisfies 21 CFR Part 11 §11.10(e) "sequence of operator ID entries" by
// making any retroactive modification detectable. VerifyChain is the
// mechanism an investigator runs during a breach-scope analysis.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"time"
)

// S3Sink describes an S3-compatible upload target.
// Authentication uses BODH_S3_ACCESS_KEY + BODH_S3_SECRET_KEY env vars via
// AWS Signature Version 4 computed with stdlib net/http — no AWS SDK.
type S3Sink struct {
	// Bucket is the S3 bucket name.
	Bucket string
	// Prefix is prepended to every object key (e.g. "audit/").
	Prefix string
	// Endpoint overrides the AWS endpoint; set for MinIO or GCS.
	// Empty string uses the standard AWS regional endpoint.
	Endpoint string
	// Region is the AWS region (e.g. "us-east-1").
	Region string
}

// ChainedEvent is one line in the JSONL audit file.
// The hash chain is: Hash = SHA-256(prev_hash_hex + "\n" + json(Event)).
// This construction is append-friendly — the full chain is verifiable from
// the first event to the last without any external state.
type ChainedEvent struct {
	Event    Event  `json:"event"`
	PrevHash string `json:"prev_hash"` // hex SHA-256 of previous ChainedEvent's Hash
	Hash     string `json:"hash"`      // hex SHA-256(prev_hash + "\n" + json(Event))
	Seq      int64  `json:"seq"`       // monotonic, 0-based sequence number
}

// ImmutableRecorder wraps any Recorder with a tamper-evident JSONL file and
// optional S3 export.
type ImmutableRecorder struct {
	inner    Recorder   // may be nil; if set, every event is also forwarded
	filePath string     // path to the JSONL file on disk
	mu       sync.Mutex // serialises writes + hash state
	prevHash string     // hex SHA-256 of the last persisted event
	seq      int64      // next sequence number (read under mu)
	f        *os.File   // open append write handle
	s3       *S3Sink    // optional S3 export; nil = local only
}

// genesis is the prevHash for the very first event — a well-known all-zeros
// SHA-256 value so verifiers can reconstruct the chain from scratch.
const genesis = "0000000000000000000000000000000000000000000000000000000000000000"

// NewImmutableRecorder opens (or creates) the JSONL file at filePath, reads
// the last line to restore the chain state, and returns a ready recorder.
// inner may be nil. s3 may be nil (local-only mode).
func NewImmutableRecorder(filePath string, inner Recorder, s3 *S3Sink) (*ImmutableRecorder, error) {
	if filePath == "" {
		return nil, fmt.Errorf("audit/immutable: empty filePath")
	}
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit/immutable: open %s: %w", filePath, err)
	}
	r := &ImmutableRecorder{
		inner:    inner,
		filePath: filePath,
		prevHash: genesis,
		f:        f,
		s3:       s3,
	}
	// Restore chain state from the last line so new events continue the chain.
	if err := r.restoreChain(); err != nil {
		// Non-fatal: if the file is empty or unreadable we start a fresh chain.
		// The discrepancy will be caught by VerifyChain.
		_ = err
	}
	return r, nil
}

// restoreChain reads the last line of the JSONL file and sets prevHash + seq.
// Called once at open time; not safe to call concurrently.
func (r *ImmutableRecorder) restoreChain() error {
	f, err := os.Open(r.filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lastLine []byte
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			// copy to avoid scanner buffer reuse
			lastLine = append(lastLine[:0], scanner.Bytes()...)
		}
	}
	if len(lastLine) == 0 {
		return nil // empty file — fresh chain
	}
	var ce ChainedEvent
	if err := json.Unmarshal(lastLine, &ce); err != nil {
		return fmt.Errorf("audit/immutable: restore: %w", err)
	}
	r.prevHash = ce.Hash
	r.seq = ce.Seq + 1
	return nil
}

// Record stamps the event, computes the chain hash, appends a ChainedEvent
// JSON line, and (if configured) kicks off an async S3 upload.
func (r *ImmutableRecorder) Record(e Event) error {
	stamp(&e)

	// Encode just the Event to build the hash input.
	eventJSON, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("audit/immutable: marshal event: %w", err)
	}

	r.mu.Lock()
	seq := r.seq
	prevHash := r.prevHash

	// Hash = SHA-256(prevHash + "\n" + eventJSON)
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write([]byte("\n"))
	h.Write(eventJSON)
	hash := hex.EncodeToString(h.Sum(nil))

	ce := ChainedEvent{
		Event:    e,
		PrevHash: prevHash,
		Hash:     hash,
		Seq:      seq,
	}

	b, err := json.Marshal(ce)
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("audit/immutable: marshal chained event: %w", err)
	}

	if _, err := r.f.Write(append(b, '\n')); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("audit/immutable: write: %w", err)
	}

	// Advance chain state only after a successful write.
	r.prevHash = hash
	r.seq = seq + 1
	r.mu.Unlock()

	// Forward to inner recorder (fail-open: ignore errors).
	if r.inner != nil {
		_ = r.inner.Record(e)
	}

	// S3 fire-and-forget: upload the single event as a tiny gzipped JSONL.
	if r.s3 != nil {
		payload := append(b, '\n')
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			key := fmt.Sprintf("%saudit/%s/event-%d.jsonl.gz",
				r.s3.Prefix, time.Now().UTC().Format("2006-01-02"), seq)
			_ = s3Put(ctx, r.s3, key, payload)
		}()
	}

	return nil
}

// VerifyChain reads filePath end-to-end and validates the Merkle hash chain.
// Returns (true, 0, nil) when the chain is intact.
// Returns (false, seq, nil) when a break is detected at that seq number.
// Returns (false, -1, err) when a file or parse error prevents verification.
func (r *ImmutableRecorder) VerifyChain(filePath string) (ok bool, brokenAt int64, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return false, -1, fmt.Errorf("audit/immutable: verify open: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	prevHash := genesis
	var lineNum int64

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ce ChainedEvent
		if err := json.Unmarshal(line, &ce); err != nil {
			return false, lineNum, fmt.Errorf("audit/immutable: verify parse line %d: %w", lineNum, err)
		}

		// Recompute hash from the stored event JSON.
		eventJSON, err := json.Marshal(ce.Event)
		if err != nil {
			return false, ce.Seq, fmt.Errorf("audit/immutable: verify re-marshal seq %d: %w", ce.Seq, err)
		}

		h := sha256.New()
		h.Write([]byte(prevHash))
		h.Write([]byte("\n"))
		h.Write(eventJSON)
		expected := hex.EncodeToString(h.Sum(nil))

		if !hmac.Equal([]byte(ce.Hash), []byte(expected)) {
			return false, ce.Seq, nil
		}
		if ce.PrevHash != prevHash {
			return false, ce.Seq, nil
		}

		prevHash = ce.Hash
		lineNum++
	}
	if err := scanner.Err(); err != nil {
		return false, lineNum, fmt.Errorf("audit/immutable: verify scan: %w", err)
	}
	return true, 0, nil
}

// ExportToS3 uploads the last N lines of the audit file to S3 as a gzipped
// JSONL object. If lines <= 0, all lines are uploaded.
// Key format: {prefix}audit/{date}/{first_seq}-{last_seq}.jsonl.gz
func (r *ImmutableRecorder) ExportToS3(ctx context.Context, lines int) error {
	if r.s3 == nil {
		return fmt.Errorf("audit/immutable: S3 not configured")
	}

	f, err := os.Open(r.filePath)
	if err != nil {
		return fmt.Errorf("audit/immutable: export open: %w", err)
	}
	defer f.Close()

	// Collect all lines.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var all [][]byte
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			cp := make([]byte, len(scanner.Bytes()))
			copy(cp, scanner.Bytes())
			all = append(all, cp)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("audit/immutable: export scan: %w", err)
	}

	if lines > 0 && lines < len(all) {
		all = all[len(all)-lines:]
	}
	if len(all) == 0 {
		return nil // nothing to upload
	}

	// Determine seq range for the key.
	var firstSeq, lastSeq int64
	var first, last ChainedEvent
	_ = json.Unmarshal(all[0], &first)
	_ = json.Unmarshal(all[len(all)-1], &last)
	firstSeq, lastSeq = first.Seq, last.Seq

	// Gzip the selected lines.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	for _, ln := range all {
		gz.Write(ln)
		gz.Write([]byte("\n"))
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("audit/immutable: gzip: %w", err)
	}

	key := fmt.Sprintf("%saudit/%s/%d-%d.jsonl.gz",
		r.s3.Prefix, time.Now().UTC().Format("2006-01-02"), firstSeq, lastSeq)

	return s3Put(ctx, r.s3, key, buf.Bytes())
}

// Close flushes and closes the underlying file. Safe to call multiple times.
func (r *ImmutableRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// List satisfies the Recorder interface by delegating to the inner recorder
// (if set) or returning nil. ImmutableRecorder does not cache events in
// memory; callers should use VerifyChain + direct file reads for forensics.
func (r *ImmutableRecorder) List() []Event {
	if r.inner != nil {
		return r.inner.List()
	}
	return nil
}

// ----------------------------------------------------------------------------
// S3 upload via stdlib net/http + AWS Signature Version 4
// ----------------------------------------------------------------------------

// s3Put uploads payload to the configured S3-compatible endpoint using a
// pre-computed AWS SigV4 Authorization header. No AWS SDK is used.
// Credentials are read from BODH_S3_ACCESS_KEY + BODH_S3_SECRET_KEY env vars.
func s3Put(ctx context.Context, sink *S3Sink, key string, payload []byte) error {
	accessKey := os.Getenv("BODH_S3_ACCESS_KEY")
	secretKey := os.Getenv("BODH_S3_SECRET_KEY")

	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	endpoint := sink.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", sink.Region)
	}
	url := fmt.Sprintf("%s/%s/%s", endpoint, sink.Bucket, key)

	// Compute payload hash.
	payloadHash := sha256Hex(payload)

	// Canonical headers (must be sorted by header name).
	canonicalHeaders := fmt.Sprintf(
		"content-type:application/octet-stream\nhost:%s\nx-amz-content-sha256:%s\nx-amz-date:%s\n",
		hostFromEndpoint(endpoint, sink.Bucket), payloadHash, amzDate,
	)
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"

	canonicalReq := fmt.Sprintf("PUT\n/%s/%s\n\n%s\n%s\n%s",
		sink.Bucket, key, canonicalHeaders, signedHeaders, payloadHash)

	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, sink.Region)
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate, credentialScope, sha256Hex([]byte(canonicalReq)))

	signingKey := deriveSigningKey(secretKey, dateStamp, sink.Region, "s3")
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authHeader := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s,SignedHeaders=%s,Signature=%s",
		accessKey, credentialScope, signedHeaders, signature,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("audit/s3: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("Authorization", authHeader)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("audit/s3: PUT: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("audit/s3: PUT %s → %d: %s", url, resp.StatusCode, body)
	}
	return nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func deriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

// hostFromEndpoint extracts the HTTP host for the canonical header.
func hostFromEndpoint(endpoint, bucket string) string {
	// Strip scheme.
	for _, pfx := range []string{"https://", "http://"} {
		if len(endpoint) > len(pfx) && endpoint[:len(pfx)] == pfx {
			return endpoint[len(pfx):]
		}
	}
	return endpoint
}
