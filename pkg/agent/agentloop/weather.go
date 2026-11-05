package agentloop

import (
	"encoding/json"
	"fmt"

	"github.com/PratikDhanave/bodhyanga/pkg/llm"
)

var getWeatherTool = localTool{
	Tool: llm.Tool{
		Name:        "getWeather",
		Description: "Get the current weather for a city. Returns temperature in Celsius and a short description.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"city":{"type":"string","description":"City name, e.g. London"}},"required":["city"]}`),
	},
	Execute: func(args map[string]any) (string, error) {
		city, _ := args["city"].(string)
		if city == "" {
			return "", fmt.Errorf("city is required")
		}
		// Mock response — no real API call needed for the lesson.
		return fmt.Sprintf(`{"city":%q,"temp_c":18,"description":"partly cloudy"}`, city), nil
	},
}
