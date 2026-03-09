package api

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Priority levels
const (
	PriorityEnrich = iota
	PrioritySync
	PriorityUser
)

type apiTask struct {
	id       uint64
	priority int
	fn       types.TaskFunc
	resp     chan tea.Msg
	ctx      context.Context
}

// APITrafficController ensures prioritized access to the GitHub API.
type APITrafficController struct {
	ctx    context.Context // Application root context
	logger *slog.Logger
	mu     sync.Mutex
	userMu sync.Mutex // Serializes PriorityUser tasks to prevent out-of-order writes
	wg     sync.WaitGroup

	taskCounter uint64

	// Channels for prioritized tasks
	high chan *apiTask // Fast Track for PriorityUser
	med  chan *apiTask
	low  chan *apiTask

	// Threshold for pausing background tasks (Enrichment)
	rateLimitThreshold int32
	remainingRateLimit int32

	// Concurrency scaling
	workerLimit int32
	sem         chan struct{} // Controls active workers
	adj         chan int32    // Channel for scaling requests
}

func NewAPITrafficController(ctx context.Context, logger *slog.Logger) *APITrafficController {
	const maxConcurrency = 3
	tc := &APITrafficController{
		ctx:                ctx,
		logger:             logger,
		high:               make(chan *apiTask, 10),
		med:                make(chan *apiTask, 10),
		low:                make(chan *apiTask, 100),
		rateLimitThreshold: 500,
		remainingRateLimit: 5000,
		workerLimit:        maxConcurrency,
		sem:                make(chan struct{}, maxConcurrency),
		adj:                make(chan int32, 1),
	}

	// Fill semaphore initially
	for i := 0; i < maxConcurrency; i++ {
		tc.sem <- struct{}{}
	}

	// Start concurrency manager
	go tc.concurrencyManager(ctx)

	// Launch worker pool
	for i := 0; i < maxConcurrency; i++ {
		tc.wg.Add(1)
		// #nosec G118: Background worker intended to outlive request context
		go tc.worker(ctx, i)
	}
	return tc
}

func (c *APITrafficController) concurrencyManager(ctx context.Context) {
	current := int32(3)
	for {
		select {
		case <-ctx.Done():
			return
		case target := <-c.adj:
			if target == current {
				continue
			}

			if target < current {
				// Scale down: Drain semaphore
				for i := 0; i < int(current-target); i++ {
					select {
					case <-ctx.Done():
						return
					case <-c.sem:
					}
				}
				c.logger.Info("traffic controller: scaled down concurrency", "new_limit", target)
			} else {
				// Scale up: Fill semaphore
				for i := 0; i < int(target-current); i++ {
					select {
					case <-ctx.Done():
						return
					case c.sem <- struct{}{}:
					}
				}
				c.logger.Info("traffic controller: scaled up concurrency", "new_limit", target)
			}
			current = target
			atomic.StoreInt32(&c.workerLimit, target)
		}
	}
}

func (c *APITrafficController) worker(ctx context.Context, id int) {
	defer c.wg.Done()
	for {
		// Binary Scaler: Wait for slot in semaphore
		select {
		case <-ctx.Done():
			return
		case <-c.sem:
			// Acquired slot, proceed to pull task
		}

		var task *apiTask
		var ok bool

		// Priority Select: Always check High (Fast Track) first
		select {
		case <-ctx.Done():
			c.sem <- struct{}{} // Return slot
			return
		case task, ok = <-c.high:
		default:
			select {
			case <-ctx.Done():
				c.sem <- struct{}{} // Return slot
				return
			case task, ok = <-c.high:
			case task, ok = <-c.med:
			case task, ok = <-c.low:
			}
		}

		if !ok {
			c.sem <- struct{}{}
			return
		}

		if c.logger.Enabled(ctx, slog.LevelDebug) {
			c.logger.DebugContext(ctx, "traffic controller: worker dispatched task", "worker_id", id, "task_id", task.id, "priority", task.priority)
		}
		c.executeTask(ctx, task)

		// Release slot back to semaphore
		c.sem <- struct{}{}
	}
}

func (c *APITrafficController) executeTask(ctx context.Context, t *apiTask) {
	// 1. Serialization for User Actions
	if t.priority == PriorityUser {
		c.userMu.Lock()
		defer c.userMu.Unlock()
	}

	// 2. Rate Limit Guard (using atomic)
	remaining := atomic.LoadInt32(&c.remainingRateLimit)
	threshold := atomic.LoadInt32(&c.rateLimitThreshold)

	if t.priority == PriorityEnrich && remaining < threshold {
		c.logger.WarnContext(ctx, "traffic controller: skipping enrichment due to low quota", "task_id", t.id, "remaining", remaining)
		t.resp <- nil
		return
	}

	// 3. Execution
	taskCtx := t.ctx
	if taskCtx == nil {
		taskCtx = ctx
	}

	tracer := config.GetTracer()
	_, span := tracer.Start(taskCtx, "traffic_controller.execute",
		trace.WithAttributes(
			attribute.String("task_id", fmt.Sprintf("%d", t.id)),
			attribute.Int("priority", t.priority),
		),
	)
	defer span.End()

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	msg := t.fn(execCtx)
	t.resp <- msg
}

// UpdateRateLimit updates the internal tracking of the GitHub quota and adjusts concurrency.
func (c *APITrafficController) UpdateRateLimit(ctx context.Context, remaining int) {
	// #nosec G115: GitHub rate limits are well within int32 range (max 5000-15000)
	val := int32(remaining)
	atomic.StoreInt32(&c.remainingRateLimit, val)
	if c.logger.Enabled(ctx, slog.LevelDebug) {
		c.logger.DebugContext(ctx, "traffic controller: updated rate limit", "remaining", remaining)
	}

	// Dynamic Scaling Logic
	c.adjustConcurrency(remaining)
}

func (c *APITrafficController) adjustConcurrency(remaining int) {
	target := int32(3)
	if remaining <= 500 {
		target = 1
	}

	// Send non-blocking adjustment request
	select {
	case c.adj <- target:
	default:
		// Manager already busy or queue full, skip this cycle
	}
}

// Remaining returns the last known remaining rate limit.
func (c *APITrafficController) Remaining() int {
	return int(atomic.LoadInt32(&c.remainingRateLimit))
}

// Shutdown waits for the worker to finish processing.
func (c *APITrafficController) Shutdown(ctx context.Context) {
	c.wg.Wait()
	c.logger.DebugContext(ctx, "traffic controller: shutdown complete")
}

// Submit wraps an API operation in a serialized, prioritized command.
func (c *APITrafficController) Submit(priority int, fn types.TaskFunc) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		c.taskCounter++
		id := c.taskCounter
		c.mu.Unlock()

		task := &apiTask{
			id:       id,
			priority: priority,
			fn:       fn,
			resp:     make(chan tea.Msg, 1),
			ctx:      c.ctx, // Carry root application context for trace linkage
		}

		switch priority {
		case PriorityUser:
			c.high <- task
		case PrioritySync:
			c.med <- task
		default:
			c.low <- task
		}

		return <-task.resp
	}
}
