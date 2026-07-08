package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
)

// transport is the JSON-RPC over HTTP+SSE round-trip implementation. One
// transport instance is owned by an HTTPClient; methods are safe for
// concurrent use because each call gets a fresh ID via the atomic counter.
type transport struct {
	endpoint string
	http     *http.Client
	auth     AuthProvider
	next     atomic.Int64
}

// nextID returns a monotonically increasing JSON-RPC id, encoded as a JSON
// number. Each Request gets a unique id so a future bidirectional / batched
// transport variant can multiplex responses.
func (t *transport) nextID() json.RawMessage {
	id := t.next.Add(1)
	return json.RawMessage(fmt.Sprintf("%d", id))
}

// call sends one JSON-RPC request and decodes the response. It handles both
// modes:
//
//   - Direct JSON body (Content-Type: application/json) — the simple
//     request/response case. The whole body is parsed as one Response.
//
//   - Server-sent events (Content-Type: text/event-stream) — for streaming
//     tool results. Each "data:" line in the stream is one JSON object; for
//     tools/call the server may emit multiple ToolResult chunks which we
//     concatenate into a single final ToolResult. Any chunk that's a full
//     JSON-RPC response wins immediately (we use the last non-error response
//     in the stream).
//
// Returns the decoded Response, or an error for transport / decode / context
// failures. A JSON-RPC-level error is surfaced inside Response.Error and
// must be checked by the caller (decodeResult does that for typed callers).
func (t *transport) call(ctx context.Context, method string, params any) (*Response, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		buf, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("mcp: marshal params for %s: %w", method, err)
		}
		paramsRaw = buf
	}

	reqEnv := Request{
		JSONRPC: JSONRPCVersion,
		ID:      t.nextID(),
		Method:  method,
		Params:  paramsRaw,
	}
	body, err := json.Marshal(reqEnv)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal envelope for %s: %w", method, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp: build http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	if t.auth != nil {
		if err := t.auth.Apply(httpReq); err != nil {
			return nil, fmt.Errorf("mcp: auth: %w", err)
		}
	}

	resp, err := t.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// Read up to 4KB of body for the error message; bigger payloads on
		// 4xx/5xx are almost always provider HTML / verbose stacks we don't
		// want to surface to clinical logs.
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("mcp: http %d: %s", resp.StatusCode, strings.TrimSpace(string(preview)))
	}

	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		return t.readSSE(resp.Body)
	}
	return t.readJSON(resp.Body, reqEnv.ID)
}

// readJSON decodes a single Response from a JSON body and verifies the id
// matches the request id.
func (t *transport) readJSON(body io.Reader, wantID json.RawMessage) (*Response, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("mcp: read body: %w", err)
	}
	var r Response
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("mcp: decode response: %w", err)
	}
	if !bytes.Equal(bytes.TrimSpace(r.ID), bytes.TrimSpace(wantID)) {
		return nil, fmt.Errorf("mcp: id mismatch: got %s want %s", string(r.ID), string(wantID))
	}
	return &r, nil
}

// readSSE consumes a Server-Sent Events stream and assembles the final
// Response.
//
// The MCP spec allows servers to stream tools/call results as a sequence of
// SSE messages whose data lines are JSON-RPC response or partial-content
// objects. To stay compatible with multiple server styles we:
//
//   - Treat each "data:" payload as one JSON object.
//   - If the payload is a full Response (has "jsonrpc" + "id"), accumulate
//     its result content; the last response with no Error wins.
//   - If the payload is a partial ToolResult (has "content" array but no
//     "jsonrpc"), append its content chunks.
//
// The final returned Response merges any accumulated content chunks into the
// last seen JSON-RPC response. If no JSON-RPC response was seen we synthesise
// one from the chunks (id is empty in that case — callers should not rely on
// id checking for SSE).
func (t *transport) readSSE(body io.Reader) (*Response, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		lastResp    *Response
		accumChunks []ContentItem
		isError     bool
		anyPayload  bool
	)

	// SSE format: blank-line separated event blocks; lines starting with
	// "data:" carry the payload, lines starting with "event:" name the event,
	// lines starting with ":" are comments. Multi-line data is rare in
	// practice for JSON-RPC-style MCP servers, but we handle it by
	// concatenating successive data: lines within one block.
	var dataBuf strings.Builder
	flush := func() error {
		if dataBuf.Len() == 0 {
			return nil
		}
		payload := strings.TrimSpace(dataBuf.String())
		dataBuf.Reset()
		if payload == "" {
			return nil
		}
		anyPayload = true

		// First try: is this a full JSON-RPC response?
		var maybeResp Response
		if err := json.Unmarshal([]byte(payload), &maybeResp); err == nil && maybeResp.JSONRPC == JSONRPCVersion {
			if maybeResp.Error != nil {
				// Error responses end the stream.
				return maybeResp.Error
			}
			lastResp = &maybeResp
			return nil
		}

		// Second try: is this a partial ToolResult chunk?
		var chunk ToolResult
		if err := json.Unmarshal([]byte(payload), &chunk); err == nil && len(chunk.Content) > 0 {
			accumChunks = append(accumChunks, chunk.Content...)
			if chunk.IsError {
				isError = true
			}
			return nil
		}

		// Third try: is this a bare ContentItem?
		var item ContentItem
		if err := json.Unmarshal([]byte(payload), &item); err == nil && item.Type != "" {
			accumChunks = append(accumChunks, item)
			return nil
		}

		return fmt.Errorf("mcp: unrecognised SSE payload: %s", truncateForLog(payload))
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, ":"):
			// Comment, ignore.
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimPrefix(line, "data:")
			// SSE permits a leading space after the colon; trim it.
			data = strings.TrimPrefix(data, " ")
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(data)
		default:
			// Other SSE fields (event:, id:, retry:) and unknown lines carry
			// nothing relevant to a JSON-RPC payload; ignore per the SSE spec.
		}
	}
	// Flush any trailing data block.
	if err := flush(); err != nil {
		return nil, err
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("mcp: read sse: %w", err)
	}
	if !anyPayload {
		return nil, errors.New("mcp: sse stream contained no payloads")
	}

	// Merge accumulated chunks into the response, if any.
	if len(accumChunks) > 0 {
		merged := ToolResult{Content: accumChunks, IsError: isError}
		mergedRaw, _ := json.Marshal(merged)
		if lastResp == nil {
			return &Response{JSONRPC: JSONRPCVersion, Result: mergedRaw}, nil
		}
		// If the final JSON-RPC response also carried content, prefer the
		// merged accumulator since chunked streaming semantics override any
		// terminal whole-result envelope.
		lastResp.Result = mergedRaw
	}
	if lastResp == nil {
		return nil, errors.New("mcp: sse stream ended without a response")
	}
	return lastResp, nil
}

// decodeResult unmarshals resp.Result into v after checking for a JSON-RPC
// error. Returns *Error for JSON-RPC failures, wrapped errors otherwise.
func decodeResult(resp *Response, v any) error {
	if resp == nil {
		return errors.New("mcp: nil response")
	}
	if resp.Error != nil {
		return resp.Error
	}
	if v == nil {
		return nil
	}
	if len(resp.Result) == 0 {
		// Empty result is legal — leave v at its zero value.
		return nil
	}
	if err := json.Unmarshal(resp.Result, v); err != nil {
		return fmt.Errorf("mcp: decode result: %w", err)
	}
	return nil
}

// truncateForLog clamps long strings so error messages never balloon.
func truncateForLog(s string) string {
	const max = 240
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
