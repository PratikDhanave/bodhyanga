package llm

import "context"

// StreamChunk is one incremental piece of a streaming LLM response.
type StreamChunk struct {
	// Delta is the new text added in this chunk (may be empty on the final chunk).
	Delta string

	// Done is true on the final chunk. When Done=true, FullContent contains
	// the complete assembled response text.
	Done        bool
	FullContent string

	// Err is set if the stream encountered an error mid-flight. When Err is
	// non-nil, no further chunks will be sent and the channel is closed.
	Err error
}

// Streamer is an optional interface for LLM clients that support token
// streaming. Not all providers implement it. Use AsStreamer to check:
//
//	if s, ok := llm.AsStreamer(client); ok {
//	    ch, err := s.Stream(ctx, req)
//	    if err != nil { ... }
//	    for chunk := range ch {
//	        if chunk.Err != nil { ... }
//	        fmt.Print(chunk.Delta)
//	    }
//	}
//
// Providers that implement Streamer: Ollama (this file), Anthropic, OpenAI.
// The kronk/in-process provider does not stream.
type Streamer interface {
	// Stream sends a completion request and returns a channel of incremental
	// response chunks. The channel is closed after the final (Done=true) chunk
	// or when an error is sent. The caller must drain the channel to avoid
	// goroutine leaks.
	//
	// Unlike Complete, Stream never buffers the full response before returning.
	// The first token typically arrives within milliseconds, dramatically
	// improving perceived latency for long responses.
	Stream(ctx context.Context, req Request) (<-chan StreamChunk, error)
}

// AsStreamer returns the Streamer interface if the client supports it, and
// false otherwise. This lets callers degrade gracefully to Complete when
// streaming is unavailable.
func AsStreamer(c Client) (Streamer, bool) {
	s, ok := c.(Streamer)
	return s, ok
}
