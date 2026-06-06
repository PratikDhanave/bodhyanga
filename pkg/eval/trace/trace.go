package trace

import "time"

// Trace represents one end-to-end execution of the agent pipeline, from user
// query to final output. It captures all intermediate spans (LLM calls, tool
// calls, retrieval steps) along with timing, metadata, and optional user
// feedback.
type Trace struct {
	ID           string         `json:"id"`
	Query        string         `json:"query"`
	Spans        []Span         `json:"spans"`
	FinalOutput  string         `json:"final_output"`
	Metadata     map[string]any `json:"metadata"`
	Duration     time.Duration  `json:"duration"`
	Timestamp    time.Time      `json:"timestamp"`
	UserFeedback *string        `json:"user_feedback,omitempty"` // "thumbs_up" / "thumbs_down"
}

// Span is a single unit of work inside a Trace.
type Span struct {
	Name     string         `json:"name"` // "llm_call", "tool_call", "retrieval"
	Input    string         `json:"input"`
	Output   string         `json:"output"`
	Duration time.Duration  `json:"duration"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// TraceStats summarises a corpus of traces.
type TraceStats struct {
	TotalTraces  int
	AvgDuration  time.Duration
	P50Duration  time.Duration
	P95Duration  time.Duration
	P99Duration  time.Duration
	AvgSpans     float64
	AvgToolCalls float64
	FeedbackRate float64                    // fraction of traces with user feedback
	ThumbsUpRate float64                    // of those with feedback, fraction thumbs-up
	ByMetadata   map[string]map[string]int  // e.g. {"persona": {"investor": 42}}
}

// OutlierReport flags a single trace that exceeds normal bounds for a given
// metric.
type OutlierReport struct {
	TraceID   string
	Metric    string
	Value     float64
	Threshold float64
	Direction string // "above" or "below"
}
