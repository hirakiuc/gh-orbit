package api

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestTrafficController_Concurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		logger := slog.Default()
		tc := NewAPITrafficController(ctx, logger)
		defer tc.Shutdown(ctx)

		const taskCount = 20
		var wg sync.WaitGroup
		wg.Add(taskCount)

		start := time.Now()

		// Submit many tasks simultaneously
		for i := 0; i < taskCount; i++ {
			go func(id int) {
				defer wg.Done()
				resChan, err := tc.Submit(context.Background(), PriorityEnrich, func(ctx context.Context) any {
					time.Sleep(50 * time.Millisecond) // Simulate API latency
					return nil
				})
				assert.NoError(t, err)
				<-resChan
			}(i)
		}

		wg.Wait()
		duration := time.Since(start)

		// With 3 workers and 20 tasks of 50ms each:
		// Serial: 20 * 50ms = 1000ms
		// Concurrent (3 workers): 7 batches * 50ms = 350ms
		// We allow some delta for supervisor ticker timing (2ms per batch)
		assert.InDelta(t, 350*time.Millisecond, duration, float64(20*time.Millisecond), "Execution should be deterministic in synctest")

		cancel()
	})
}

func TestTrafficController_UserPriorityPreemption(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		logger := slog.Default()
		tc := NewAPITrafficController(ctx, logger)
		defer tc.Shutdown(ctx)

		// 1. Flood with low priority slow tasks
		for i := 0; i < 10; i++ {
			_, _ = tc.Submit(context.Background(), PriorityEnrich, func(ctx context.Context) any {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
		}

		// 2. Submit a high priority task
		start := time.Now()
		highDone := make(chan struct{})
		go func() {
			resChan, err := tc.Submit(context.Background(), PriorityUser, func(ctx context.Context) any {
				return nil
			})
			assert.NoError(t, err)
			<-resChan
			close(highDone)
		}()

		// High priority task should finish quickly despite the queue
		// In synctest, it should be near-instant (0ms) or at most the first worker release
		select {
		case <-highDone:
			duration := time.Since(start)
			assert.LessOrEqual(t, duration, 150*time.Millisecond, "High priority task should preempt enrichment deterministically")
		case <-time.After(500 * time.Millisecond):
			t.Fatal("High priority task timed out - likely blocked by enrichment")
		}

		cancel()
	})
}

func TestTrafficController_NotificationBatchRunsBeforeQueuedScalarAtLimitOne(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tc := NewAPITrafficController(ctx, slog.Default())
	atomic.StoreInt32(&tc.workerLimit, 1)
	t.Cleanup(func() { tc.Shutdown(context.Background()) })

	prepared := make(chan struct{})
	childStarted := make(chan struct{})
	releaseChild := make(chan struct{})
	batchDone := make(chan notificationBatchExecution, 1)
	go func() {
		result, _ := tc.RunNotificationBatch(ctx, notificationBatchPlan{
			Request: types.NotificationBatchRequest{Operation: types.NotificationBatchRead, IDs: []string{"1"}},
			Prepare: func(context.Context) error { close(prepared); return nil },
			Remote: func(context.Context, string) error {
				close(childStarted)
				<-releaseChild
				return nil
			},
		})
		batchDone <- result
	}()
	<-prepared
	<-childStarted

	scalarStarted := make(chan struct{})
	scalarResult, err := tc.Submit(ctx, PriorityUser, func(context.Context) any {
		close(scalarStarted)
		return "scalar"
	})
	assert.NoError(t, err)
	select {
	case <-scalarStarted:
		t.Fatal("scalar user work acquired capacity while the batch owned serialization")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseChild)
	result := <-batchDone
	assert.True(t, result.Committed)
	assert.Equal(t, int32(0), atomic.LoadInt32(&tc.activeWorkersCount))
	assert.Equal(t, "scalar", <-scalarResult)
	assert.Equal(t, int32(0), atomic.LoadInt32(&tc.activeWorkersCount))
}

func TestTrafficController_RateLimitAtomic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.Default()
	tc := NewAPITrafficController(ctx, logger)

	// Stress the atomic updates
	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			tc.UpdateRateLimit(ctx, models.RateLimitInfo{Remaining: i})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = tc.Remaining()
		}
	}()

	wg.Wait()
}

func TestTrafficController_ScalingStress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		logger := slog.Default()
		tc := NewAPITrafficController(ctx, logger)
		defer tc.Shutdown(ctx)

		// 1. Submit a continuous stream of background tasks
		stopTasks := make(chan struct{})
		var tasksCompleted int64
		go func() {
			for {
				select {
				case <-stopTasks:
					return
				default:
					_, err := tc.Submit(context.Background(), PriorityEnrich, func(ctx context.Context) any {
						time.Sleep(10 * time.Millisecond)
						atomic.AddInt64(&tasksCompleted, 1)
						return nil
					})
					// resChan is buffered, so we don't need to read from it to let worker finish
					_ = err
					time.Sleep(2 * time.Millisecond)
				}
			}
		}()

		// 2. Rapidly fluctuate rate limit to trigger scaling
		for i := 0; i < 10; i++ {
			// Scale down
			tc.UpdateRateLimit(ctx, models.RateLimitInfo{Limit: 5000, Remaining: 100})
			time.Sleep(20 * time.Millisecond)

			// Scale up
			tc.UpdateRateLimit(ctx, models.RateLimitInfo{Limit: 5000, Remaining: 1000})
			time.Sleep(20 * time.Millisecond)
		}

		close(stopTasks)
		time.Sleep(100 * time.Millisecond) // Allow final tasks to settle

		// If we haven't deadlocked, we pass
		finalLimit := atomic.LoadInt32(&tc.workerLimit)
		assert.Equal(t, int32(3), finalLimit, "Should settle at max concurrency")

		cancel()
	})
}

func TestTrafficController_SubmitReturnsQueueFullInsteadOfBlocking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		testCases := []struct {
			name     string
			priority int
			capacity int
		}{
			{name: "user", priority: PriorityUser, capacity: 10},
			{name: "sync", priority: PrioritySync, capacity: 50},
			{name: "enrich", priority: PriorityEnrich, capacity: 100},
		}

		for _, tcDef := range testCases {
			ctx, cancel := context.WithCancel(context.Background())
			tc := NewAPITrafficController(ctx, slog.Default())

			atomic.StoreInt32(&tc.workerLimit, 0)
			synctest.Wait()

			for i := 0; i < tcDef.capacity; i++ {
				resChan, err := tc.Submit(context.Background(), tcDef.priority, func(ctx context.Context) any { return nil })
				assert.NoErrorf(t, err, "%s queue should accept task %d within capacity", tcDef.name, i)
				assert.NotNilf(t, resChan, "%s queue should return response channel within capacity", tcDef.name)
			}

			resChan, err := tc.Submit(context.Background(), tcDef.priority, func(ctx context.Context) any { return nil })
			assert.Nilf(t, resChan, "%s queue should reject task when full", tcDef.name)
			assert.ErrorIsf(t, err, ErrTrafficQueueFull, "%s queue should fail fast when full", tcDef.name)

			tc.Shutdown(ctx)
			cancel()
		}
	})
}

func TestTrafficController_SubmitStillEnqueuesWhenCapacityAvailable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tc := NewAPITrafficController(ctx, slog.Default())
		defer tc.Shutdown(ctx)

		resChan, err := tc.Submit(context.Background(), PrioritySync, func(ctx context.Context) any {
			return "ok"
		})
		assert.NoError(t, err)
		assert.Equal(t, "ok", <-resChan)
	})
}

func TestTrafficController_SubmitAlreadyCanceledContextSkipsExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		tc := NewAPITrafficController(ctx, slog.Default())
		defer tc.Shutdown(ctx)

		atomic.StoreInt32(&tc.workerLimit, 0)
		synctest.Wait()

		taskCtx, cancel := context.WithCancel(context.Background())
		cancel()

		executed := make(chan struct{}, 1)
		resChan, err := tc.Submit(taskCtx, PrioritySync, func(ctx context.Context) any {
			close(executed)
			return "unexpected"
		})
		assert.NoError(t, err)
		synctest.Wait()

		assert.Equal(t, 0, len(tc.med), "already-canceled tasks should not occupy the queue")
		assert.Nil(t, <-resChan)
		assert.Empty(t, executed)
	})
}

func TestTrafficController_QueuedCanceledContextSkipsExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		tc := NewAPITrafficController(ctx, slog.Default())
		defer tc.Shutdown(ctx)

		atomic.StoreInt32(&tc.workerLimit, 0)
		synctest.Wait()

		taskCtx, cancel := context.WithCancel(context.Background())
		executed := make(chan struct{}, 1)
		resChan, err := tc.Submit(taskCtx, PrioritySync, func(ctx context.Context) any {
			close(executed)
			return "unexpected"
		})
		assert.NoError(t, err)

		cancel()
		atomic.StoreInt32(&tc.workerLimit, 1)
		synctest.Wait()

		assert.Nil(t, <-resChan)
		assert.Empty(t, executed)
	})
}

func TestTrafficController_RunningTaskObservesRequestCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		tc := NewAPITrafficController(ctx, slog.Default())
		defer tc.Shutdown(ctx)

		taskCtx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		resChan, err := tc.Submit(taskCtx, PriorityUser, func(ctx context.Context) any {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		assert.NoError(t, err)

		<-started
		cancel()
		synctest.Wait()

		assert.ErrorIs(t, (<-resChan).(error), context.Canceled)
	})
}
