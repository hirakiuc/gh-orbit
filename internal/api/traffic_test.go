package api

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

func TestTrafficController_Serialization(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logger := slog.Default()
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		tc := NewAPITrafficController(ctx, logger)

		var mu sync.Mutex
		var sequence []int
		var wg sync.WaitGroup
		wg.Add(3)

		// Submit 3 tasks with different priorities
		cmd1 := tc.Submit(PriorityEnrich, func(ctx context.Context) tea.Msg {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			mu.Lock()
			sequence = append(sequence, PriorityEnrich)
			mu.Unlock()
			return nil
		})
		cmd2 := tc.Submit(PrioritySync, func(ctx context.Context) tea.Msg {
			defer wg.Done()
			mu.Lock()
			sequence = append(sequence, PrioritySync)
			mu.Unlock()
			return nil
		})
		cmd3 := tc.Submit(PriorityUser, func(ctx context.Context) tea.Msg {
			defer wg.Done()
			mu.Lock()
			sequence = append(sequence, PriorityUser)
			mu.Unlock()
			return nil
		})

		// Run them in parallel
		go cmd1()
		go cmd2()
		go cmd3()

		// Wait for execution
		synctest.Wait()
		wg.Wait()

		mu.Lock()
		defer mu.Unlock()
		assert.Len(t, sequence, 3, "Expected 3 tasks to complete")
	})
}

func TestTrafficController_RateLimitGuard(t *testing.T) {
	logger := slog.Default()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	tc := NewAPITrafficController(ctx, logger)
	tc.UpdateRateLimit(ctx, 100) // Below default threshold (500)

	var enriched bool
	cmd := tc.Submit(PriorityEnrich, func(ctx context.Context) tea.Msg {
		enriched = true
		return nil
	})

	_ = cmd()

	if enriched {
		t.Error("Expected enrichment task to be skipped due to low rate limit")
	}
}
