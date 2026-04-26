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
				resChan, err := tc.Submit(PriorityEnrich, func(ctx context.Context) any {
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
			_, _ = tc.Submit(PriorityEnrich, func(ctx context.Context) any {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
		}

		// 2. Submit a high priority task
		start := time.Now()
		highDone := make(chan struct{})
		go func() {
			resChan, err := tc.Submit(PriorityUser, func(ctx context.Context) any {
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
			assert.LessOrEqual(t, duration, 100*time.Millisecond, "High priority task should preempt enrichment deterministically")
		case <-time.After(500 * time.Millisecond):
			t.Fatal("High priority task timed out - likely blocked by enrichment")
		}

		cancel()
	})
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
					_, err := tc.Submit(PriorityEnrich, func(ctx context.Context) any {
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
