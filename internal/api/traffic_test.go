package api

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestTrafficController_Concurrency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.Default()
	tc := NewAPITrafficController(ctx, logger)

	const taskCount = 20
	var wg sync.WaitGroup
	wg.Add(taskCount)

	start := time.Now()

	// Submit many tasks simultaneously
	for i := 0; i < taskCount; i++ {
		go func(id int) {
			defer wg.Done()
			cmd := tc.Submit(PriorityEnrich, func(ctx context.Context) tea.Msg {
				time.Sleep(50 * time.Millisecond) // Simulate API latency
				return nil
			})
			_ = cmd()
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	// With 3 workers and 20 tasks of 50ms each:
	// Serial: 20 * 50ms = 1000ms
	// Concurrent (3 workers): ceil(20/3) * 50ms = 7 * 50ms = 350ms
	// We expect it to be significantly faster than 1s
	assert.Less(t, duration, 600*time.Millisecond, "Execution should be concurrent and faster than serial")
}

func TestTrafficController_UserPriorityPreemption(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.Default()
	tc := NewAPITrafficController(ctx, logger)

	// 1. Flood with low priority slow tasks
	for i := 0; i < 10; i++ {
		tc.Submit(PriorityEnrich, func(ctx context.Context) tea.Msg {
			time.Sleep(100 * time.Millisecond)
			return nil
		})
	}

	// 2. Submit a high priority task
	start := time.Now()
	highDone := make(chan struct{})
	go func() {
		cmd := tc.Submit(PriorityUser, func(ctx context.Context) tea.Msg {
			return nil
		})
		_ = cmd()
		close(highDone)
	}()

	// High priority task should finish quickly despite the queue
	select {
	case <-highDone:
		duration := time.Since(start)
		assert.Less(t, duration, 150*time.Millisecond, "High priority task should preempt enrichment")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("High priority task timed out - likely blocked by enrichment")
	}
}

func TestTrafficController_RateLimitAtomic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.Default()
	tc := NewAPITrafficController(ctx, logger)

	// Stress the atomic updates
	const iterations = 1000
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			tc.UpdateRateLimit(ctx, i)
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
