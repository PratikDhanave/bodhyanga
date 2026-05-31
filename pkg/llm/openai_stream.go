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

// Stream implements Streamer for openaiClient.
// It POSTs to the chat completions endpoint with "stream": true and reads SSE
// events. Each event is "data: {...}" or the terminal "data: [DONE]".
// Text deltas are in choices[0].delta.content.
func (c *openaiClient) Stream(ctx context.Context, req Request) (<-chan StreamChunk, error) {
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
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai stream: %w", err)
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("openai stream: http %d", resp.StatusCode)
	}

	ch := make(chan StreamChunk, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		var full string
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				select {
				case ch <- StreamChunk{Done: true, FullContent: full}:
				case <-ctx.Done():
					ch <- StreamChunk{Err: ctx.Err()}
				}
				return
			}

			var event struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				ch <- StreamChunk{Err: fmt.Errorf("openai stream: decode: %w", err)}
				return
			}

			if len(event.Choices) == 0 {
				continue
			}
			delta := event.Choices[0].Delta.Content
			if delta != "" {
				full += delta
				select {
				case ch <- StreamChunk{Delta: delta, FullContent: full}:
				case <-ctx.Done():
					ch <- StreamChunk{Err: ctx.Err()}
					return
				}
			}

			if event.Choices[0].FinishReason != nil && *event.Choices[0].FinishReason != "" {
				select {
				case ch <- StreamChunk{Done: true, FullContent: full}:
				case <-ctx.Done():
					ch <- StreamChunk{Err: ctx.Err()}
				}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- StreamChunk{Err: fmt.Errorf("openai stream: read: %w", err)}
			return
		}
		// Send final done chunk if stream ended without [DONE].
		ch <- StreamChunk{Done: true, FullContent: full}
	}()

	return ch, nil
}
