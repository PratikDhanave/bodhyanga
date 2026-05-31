package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Stream implements Streamer for anthropicClient.
// It POSTs to the Messages API with "stream": true and reads SSE events.
// Relevant events:
//   - content_block_delta with delta.type == "text_delta": carries new text
//   - message_stop: signals end of stream
func (c *anthropicClient) Stream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
	if len(req.Messages) == 0 {
		return nil, ErrNoMessages
	}

	payload, err := c.buildPayload(req)
	if err != nil {
		return nil, err
	}
	payload["stream"] = true

	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream: %w", err)
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic stream: http %d", resp.StatusCode)
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		var full string
		scanner := bufio.NewScanner(resp.Body)
		var eventType string
		for scanner.Scan() {
			line := scanner.Text()

			// SSE format: blank line separates events; lines start with
			// "event: <type>" or "data: <json>".
			if line == "" {
				eventType = ""
				continue
			}

			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")

			switch eventType {
			case "content_block_delta":
				var event struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					ch <- StreamChunk{Err: fmt.Errorf("anthropic stream: decode: %w", err)}
					return
				}
				if event.Delta.Type == "text_delta" {
					full += event.Delta.Text
					select {
					case ch <- StreamChunk{Delta: event.Delta.Text, FullContent: full}:
					case <-ctx.Done():
						ch <- StreamChunk{Err: ctx.Err()}
						return
					}
				}

			case "message_stop":
				select {
				case ch <- StreamChunk{Done: true, FullContent: full}:
				case <-ctx.Done():
					ch <- StreamChunk{Err: ctx.Err()}
				}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Err: fmt.Errorf("anthropic stream: read: %w", err)}
			return
		}
		// If the stream ended without a message_stop event, send a final done chunk.
		ch <- StreamChunk{Done: true, FullContent: full}
	}()

	return ch, nil
}
