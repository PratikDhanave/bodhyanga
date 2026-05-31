package agentloop

import (
	"encoding/json"
	"time"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

var getDateTimeTool = localTool{
	Tool: llm.Tool{
		Name:        "getDateTime",
		Description: "Get the current date and time in UTC (ISO 8601 / RFC 3339 format).",
		Schema:      json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
	},
	Execute: func(_ map[string]any) (string, error) {
		return time.Now().UTC().Format(time.RFC3339), nil
	},
}
