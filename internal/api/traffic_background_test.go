package api

import (
	"context"
	"errors"
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
		tc.ReportRateLimit(models.RateLimitInfo{Remaining: 100})
		synctest.Wait()
		assert.Equal(t, int32(1), atomic.LoadInt32(&tc.workerLimit))

		tc.ReportRateLimit(models.RateLimitInfo{Remaining: 1000})
		synctest.Wait()
		assert.Equal(t, int32(3), atomic.LoadInt32(&tc.workerLimit))

		// 2. RunTask Lockout
		reset := time.Now().Add(time.Hour)
		tc.UpdateRateLimit(ctx, models.RateLimitInfo{Remaining: 0, Reset: reset})

		highDone := make(chan bool, 1)
		_, _ = tc.Submit(context.Background(), PriorityUser, func(ctx context.Context) any {
			highDone <- true
			return nil
		})
		synctest.Wait()
		assert.True(t, <-highDone, "User task should bypass lockout")

		res, _ := tc.Submit(context.Background(), PriorityEnrich, func(ctx context.Context) any {
			return "should-not-run"
		})
		synctest.Wait()
		assert.Nil(t, <-res, "Enrich task should be skipped during lockout")

		// 3. Dynamic Throttling low quota
		tc.UpdateRateLimit(ctx, models.RateLimitInfo{Remaining: 499})
		res, _ = tc.Submit(context.Background(), PriorityEnrich, func(ctx context.Context) any {
			return "should-not-run"
		})
		synctest.Wait()
		assert.Nil(t, <-res, "Enrich task should be skipped during low quota")
	})
}

func TestAPITrafficController_LockoutClearsAfterHealthyRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tc := NewAPITrafficController(ctx, slog.Default())
		defer tc.Shutdown(ctx)

		reset := time.Now().Add(time.Hour)
		tc.UpdateRateLimit(ctx, models.RateLimitInfo{Remaining: 0, Reset: reset})

		res, err := tc.Submit(context.Background(), PrioritySync, func(ctx context.Context) any {
			return "should-not-run"
		})
		assert.NoError(t, err)
		synctest.Wait()
		assert.Nil(t, <-res, "sync task should be skipped while lockout is active")

		tc.UpdateRateLimit(ctx, models.RateLimitInfo{Remaining: 1000, Reset: reset})

		res, err = tc.Submit(context.Background(), PrioritySync, func(ctx context.Context) any {
			return "recovered"
		})
		assert.NoError(t, err)
		synctest.Wait()
		assert.Equal(t, "recovered", <-res, "healthy update should clear stale lockout state")
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
		_, err := tc.Submit(context.Background(), PriorityUser, func(ctx context.Context) any { return nil })
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "shutdown")
	})
}

func TestAPITrafficController_ReportRateLimit_AfterShutdownDoesNotPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		tc := NewAPITrafficController(ctx, slog.Default())

		tc.Shutdown(ctx)
		synctest.Wait()

		assert.NotPanics(t, func() {
			tc.ReportRateLimit(models.RateLimitInfo{Remaining: 42})
		})
	})
}

func TestAPITrafficController_ShutdownTakesPrecedenceOverQueueFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		tc := NewAPITrafficController(ctx, slog.Default())

		atomic.StoreInt32(&tc.workerLimit, 0)
		synctest.Wait()

		for i := 0; i < cap(tc.high); i++ {
			_, err := tc.Submit(context.Background(), PriorityUser, func(ctx context.Context) any { return nil })
			assert.NoError(t, err)
		}

		tc.Shutdown(ctx)
		synctest.Wait()

		_, err := tc.Submit(context.Background(), PriorityUser, func(ctx context.Context) any { return nil })
		assert.Error(t, err)
		assert.False(t, errors.Is(err, ErrTrafficQueueFull))
		assert.Contains(t, err.Error(), "shutdown")
	})
}

func TestAPITrafficController_ShutdownWinsDuringSubmitRace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		tc := NewAPITrafficController(ctx, slog.Default())

		enteredHook := make(chan struct{}, 1)
		releaseHook := make(chan struct{})
		tc.submitBeforeLockHook = func() {
			close(enteredHook)
			<-releaseHook
		}

		result := make(chan error, 1)
		go func() {
			_, err := tc.Submit(context.Background(), PriorityUser, func(ctx context.Context) any { return nil })
			result <- err
		}()

		<-enteredHook
		tc.Shutdown(ctx)
		close(releaseHook)
		synctest.Wait()

		err := <-result
		assert.Error(t, err)
		assert.False(t, errors.Is(err, ErrTrafficQueueFull))
		assert.Contains(t, err.Error(), "shutdown")
		assert.Equal(t, 0, len(tc.high))
	})
}

func TestAPITrafficController_ShutdownCancelsRunningTaskContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		tc := NewAPITrafficController(ctx, slog.Default())

		started := make(chan struct{})
		resChan, err := tc.Submit(context.Background(), PriorityUser, func(ctx context.Context) any {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		assert.NoError(t, err)

		<-started
		tc.Shutdown(ctx)
		synctest.Wait()

		assert.ErrorIs(t, (<-resChan).(error), context.Canceled)
	})
}
