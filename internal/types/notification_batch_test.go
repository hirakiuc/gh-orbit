package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeNotificationBatchRequest(t *testing.T) {
	t.Run("trims deduplicates and sorts", func(t *testing.T) {
		request, err := NormalizeNotificationBatchRequest(NotificationBatchRequest{
			Operation: NotificationBatchHandled,
			IDs:       []string{" b ", "a", "b"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, request.IDs)
	})

	for _, test := range []struct {
		name    string
		request NotificationBatchRequest
	}{
		{name: "unsupported operation", request: NotificationBatchRequest{Operation: "toggle", IDs: []string{"1"}}},
		{name: "empty IDs", request: NotificationBatchRequest{Operation: NotificationBatchRead}},
		{name: "empty member", request: NotificationBatchRequest{Operation: NotificationBatchRead, IDs: []string{"1", " "}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeNotificationBatchRequest(test.request)
			assert.Error(t, err)
		})
	}

	t.Run("limit applies after deduplication", func(t *testing.T) {
		ids := make([]string, 0, MaxBatchNotifications+1)
		for index := 0; index < MaxBatchNotifications; index++ {
			ids = append(ids, fmt.Sprintf("%03d", index))
		}
		ids = append(ids, "000")
		_, err := NormalizeNotificationBatchRequest(NotificationBatchRequest{Operation: NotificationBatchRead, IDs: ids})
		require.NoError(t, err)

		ids[len(ids)-1] = "overflow"
		_, err = NormalizeNotificationBatchRequest(NotificationBatchRequest{Operation: NotificationBatchRead, IDs: ids})
		assert.Error(t, err)
	})
}
