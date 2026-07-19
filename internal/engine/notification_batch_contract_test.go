package engine

import (
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequiredNotificationBatchArgs(t *testing.T) {
	request := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{
		"operation": "read", "ids": []any{" b ", "a", "a"},
	}}}
	got, err := requiredNotificationBatchArgs(request)
	require.NoError(t, err)
	assert.Equal(t, types.NotificationBatchRequest{Operation: types.NotificationBatchRead, IDs: []string{"a", "b"}}, got)

	request.Params.Arguments = map[string]any{"operation": "read", "ids": []any{"a", 2}}
	_, err = requiredNotificationBatchArgs(request)
	assert.Error(t, err)
}

func TestNotificationBatchToolResultRequiresValidCommittedPayload(t *testing.T) {
	request := types.NotificationBatchRequest{Operation: types.NotificationBatchRead, IDs: []string{"a"}}
	valid := types.NotificationBatchResult{
		Status: types.NotificationBatchCommitted, Reconciliation: types.NotificationBatchAuthoritative,
		Request: request, Outcomes: []types.NotificationBatchItemResult{{ID: "a", Status: types.NotificationRemoteSucceeded}},
	}
	result, err := notificationBatchToolResult(valid, nil)
	require.NoError(t, err)
	assert.False(t, result.IsError)
	assert.NotNil(t, result.StructuredContent)

	invalid := valid
	invalid.Outcomes = nil
	result, err = notificationBatchToolResult(invalid, nil)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
