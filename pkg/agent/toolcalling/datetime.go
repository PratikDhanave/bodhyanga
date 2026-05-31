package toolcalling

import (
	"encoding/json"
	"time"

	"github.com/PratikDhanave/bodh/pkg/llm"
)

// getDateTimeTool is the "getDateTime" tool registered in this lesson.
//
// TypeScript equivalent:
//
//	export const getDateTimeTool = tool({
//	    description: "Get the current date and time",
//	    parameters:  z.object({}),
//	    execute:     async () => new Date().toISOString(),
//	})
//
// The tool takes no arguments — its JSON Schema is an empty object. When the
// model decides to call it, ExecuteTool dispatches here and returns the
// current UTC time in RFC 3339 format.
var getDateTimeTool = localTool{
	Tool: llm.Tool{
		Name:        "getDateTime",
		Description: "Get the current date and time in UTC (ISO 8601 / RFC 3339 format).",
		// Empty-object schema: no parameters. Providers interpret this as a
		// zero-argument function. We spell it out explicitly so the model sees
		// a well-formed JSON Schema and doesn't hallucinate arguments.
		Schema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "required": []
}`),
	},
	Execute: func(_ map[string]any) (string, error) {
		return time.Now().UTC().Format(time.RFC3339), nil
	},
}
