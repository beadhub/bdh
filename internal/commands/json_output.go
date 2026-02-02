package commands

import "encoding/json"

func marshalJSONOrFallback(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		return string(data) + "\n"
	}

	// Best-effort fallback: always return valid JSON for --json callers.
	fallback, fallbackErr := json.Marshal(map[string]string{
		"error": "failed to marshal JSON output",
	})
	if fallbackErr != nil {
		return "{}\n"
	}
	return string(fallback) + "\n"
}
