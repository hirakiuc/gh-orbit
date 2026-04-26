package engine

import (
	"encoding/json"
	"testing"
)

// FuzzMCPJSONParser targets the JSON-RPC parsing logic to ensure malformed
// UDS byte streams never cause a panic in the server.
func FuzzMCPJSONParser(f *testing.F) {
	// Add seed corpus
	seeds := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/list"}`,
		`{"invalid":`,
		`[1,2,3]`,
		`""`,
		`null`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var raw json.RawMessage
		// This simulates the check in handleConnection
		if err := json.Unmarshal(data, &raw); err != nil {
			return // Graceful error, not a panic
		}

		// Simulate deeper parsing of the raw message
		var msg struct {
			JSONRPC string `json:"jsonrpc"`
			ID      any    `json:"id"`
			Method  string `json:"method"`
		}
		_ = json.Unmarshal(raw, &msg)
	})
}
