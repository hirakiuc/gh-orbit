package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capabilityClientStub struct {
	result *mcp.ListToolsResult
	err    error
}

func (s capabilityClientStub) ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error) {
	return s.result, s.err
}

func TestRequireIndependentMutationTools(t *testing.T) {
	tools := func(names ...string) *mcp.ListToolsResult {
		result := &mcp.ListToolsResult{}
		for _, name := range names {
			result.Tools = append(result.Tools, mcp.Tool{Name: name})
		}
		return result
	}

	require.NoError(t, requireIndependentMutationTools(context.Background(), capabilityClientStub{result: tools("set_read", "set_handled", "batch_set_state")}))

	for _, tc := range []struct {
		name   string
		client capabilityClientStub
		want   string
	}{
		{name: "list error", client: capabilityClientStub{err: errors.New("timeout")}, want: "listing engine tools"},
		{name: "nil response", client: capabilityClientStub{}, want: "empty response"},
		{name: "missing read", client: capabilityClientStub{result: tools("set_handled", "batch_set_state")}, want: "set_read"},
		{name: "missing handled", client: capabilityClientStub{result: tools("set_read", "batch_set_state")}, want: "set_handled"},
		{name: "missing batch", client: capabilityClientStub{result: tools("set_read", "set_handled")}, want: "batch_set_state"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := requireIndependentMutationTools(context.Background(), tc.client)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestPrepareConnectedMutationClient_FailuresCloseWithoutConstructing(t *testing.T) {
	tools := func(names ...string) *mcp.ListToolsResult {
		result := &mcp.ListToolsResult{}
		for _, name := range names {
			result.Tools = append(result.Tools, mcp.Tool{Name: name})
		}
		return result
	}

	for _, tc := range []struct {
		name   string
		client capabilityClientStub
	}{
		{name: "rpc error", client: capabilityClientStub{err: errors.New("timeout")}},
		{name: "nil malformed response", client: capabilityClientStub{}},
		{name: "missing read", client: capabilityClientStub{result: tools("set_handled", "batch_set_state")}},
		{name: "missing handled", client: capabilityClientStub{result: tools("set_read", "batch_set_state")}},
		{name: "missing batch", client: capabilityClientStub{result: tools("set_read", "set_handled")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			closed, adapterConstructed, standaloneConstructed := 0, 0, 0
			err := prepareConnectedMutationClient(context.Background(), tc.client, func() error {
				closed++
				return nil
			}, func() { adapterConstructed++ })
			if err == nil {
				standaloneConstructed++
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "restart or upgrade")
			assert.Equal(t, 1, closed)
			assert.Zero(t, adapterConstructed)
			assert.Zero(t, standaloneConstructed)
		})
	}
}

func TestResolveLogLevel(t *testing.T) {
	tests := []struct {
		name      string
		baseLevel string
		isVerbose bool
		expected  slog.Level
	}{
		{
			name:      "Info level, not verbose",
			baseLevel: "info",
			isVerbose: false,
			expected:  slog.LevelInfo,
		},
		{
			name:      "Info level, verbose",
			baseLevel: "info",
			isVerbose: true,
			expected:  slog.LevelDebug,
		},
		{
			name:      "Debug level, not verbose",
			baseLevel: "debug",
			isVerbose: false,
			expected:  slog.LevelDebug,
		},
		{
			name:      "Error level, not verbose",
			baseLevel: "error",
			isVerbose: false,
			expected:  slog.LevelError,
		},
		{
			name:      "Error level, verbose",
			baseLevel: "error",
			isVerbose: true,
			expected:  slog.LevelDebug,
		},
		{
			name:      "Invalid level, not verbose defaults to info",
			baseLevel: "unknown",
			isVerbose: false,
			expected:  slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := resolveLogLevel(tt.baseLevel, tt.isVerbose)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
