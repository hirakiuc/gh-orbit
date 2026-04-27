package api

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestAPITrafficController_Background(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tc := NewAPITrafficController(ctx, slog.Default())
		defer tc.Shutdown(ctx)

		// 1. RateLimitListener scaling
		tc.RateLimitUpdates() <- models.RateLimitInfo{Remaining: 100}
		synctest.Wait()
		assert.Equal(t, int32(1), atomic.LoadInt32(&tc.workerLimit))

		tc.RateLimitUpdates() <- models.RateLimitInfo{Remaining: 1000}
		synctest.Wait()
		assert.Equal(t, int32(3), atomic.LoadInt32(&tc.workerLimit))

		// 2. RunTask Lockout
		reset := time.Now().Add(time.Hour)
		tc.UpdateRateLimit(ctx, models.RateLimitInfo{Remaining: 0, Reset: reset})

		highDone := make(chan bool, 1)
		_, _ = tc.Submit(PriorityUser, func(ctx context.Context) any {
			highDone <- true
			return nil
		})
		synctest.Wait()
		assert.True(t, <-highDone, "User task should bypass lockout")

		res, _ := tc.Submit(PriorityEnrich, func(ctx context.Context) any {
			return "should-not-run"
		})
		synctest.Wait()
		assert.Nil(t, <-res, "Enrich task should be skipped during lockout")

		// 3. Dynamic Throttling low quota
		tc.UpdateRateLimit(ctx, models.RateLimitInfo{Remaining: 499})
		res, _ = tc.Submit(PriorityEnrich, func(ctx context.Context) any {
			return "should-not-run"
		})
		synctest.Wait()
		assert.Nil(t, <-res, "Enrich task should be skipped during low quota")
	})
}

func TestAPITrafficController_Shutdown_Channels(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		tc := NewAPITrafficController(ctx, slog.Default())

		// Verify listener stops on channel closure (simulated by Shutdown)
		tc.Shutdown(ctx)
		synctest.Wait()

		// Verify Submit returns error after shutdown
		_, err := tc.Submit(PriorityUser, func(ctx context.Context) any { return nil })
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "shutdown")
	})
}
