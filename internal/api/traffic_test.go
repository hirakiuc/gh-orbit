package api

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestTrafficController_Serialization(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	tc := NewAPITrafficController(ctx, logger)

	var mu sync.Mutex
	var sequence []int

	// Submit 3 tasks with different priorities
	cmd1 := tc.Submit(PriorityEnrich, func(ctx context.Context) tea.Msg {
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		sequence = append(sequence, PriorityEnrich)
		mu.Unlock()
		return nil
	})
	cmd2 := tc.Submit(PrioritySync, func(ctx context.Context) tea.Msg {
		mu.Lock()
		sequence = append(sequence, PrioritySync)
		mu.Unlock()
		return nil
	})
	cmd3 := tc.Submit(PriorityUser, func(ctx context.Context) tea.Msg {
		mu.Lock()
		sequence = append(sequence, PriorityUser)
		mu.Unlock()
		return nil
	})

	// Run them in parallel (commands are usually run by tea.Program)
	go cmd1()
	go cmd2()
	go cmd3()

	// Wait for execution
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(sequence) != 3 {
		t.Errorf("Expected 3 tasks to complete, got %d", len(sequence))
	}
}

func TestTrafficController_RateLimitGuard(t *testing.T) {
	logger := slog.Default()
	ctx := context.Background()
	tc := NewAPITrafficController(ctx, logger)
	tc.UpdateRateLimit(100) // Below default threshold (500)

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
