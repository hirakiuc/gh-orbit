package api

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/config"
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
	fn       func(ctx context.Context) tea.Msg
	resp     chan tea.Msg
	ctx      context.Context // Trace-aware context
}

// APITrafficController ensures serialized, prioritized access to the GitHub API.
type APITrafficController struct {
	ctx    context.Context // Application root context
	logger *slog.Logger
	mu     sync.Mutex
	wg     sync.WaitGroup

	taskCounter uint64

	// Channels for prioritized tasks
	high chan *apiTask
	med  chan *apiTask
	low  chan *apiTask

	// Threshold for pausing background tasks (Enrichment)
	rateLimitThreshold int
	remainingRateLimit int
}

func NewAPITrafficController(ctx context.Context, logger *slog.Logger) *APITrafficController {
	tc := &APITrafficController{
		ctx:                ctx,
		logger:             logger,
		high:               make(chan *apiTask),
		med:                make(chan *apiTask),
		low:                make(chan *apiTask),
		rateLimitThreshold: 500,
		remainingRateLimit: 5000,
	}
	tc.wg.Add(1)
	go tc.worker(ctx)
	return tc
}

func (c *APITrafficController) worker(ctx context.Context) {
	defer c.wg.Done()
	for {
		var task *apiTask

		// Nested select for genuine preemption AND context awareness
		select {
		case <-ctx.Done():
			c.logger.Debug("traffic controller: worker stopping (context canceled)")
			return
		case task = <-c.high:
		default:
			select {
			case <-ctx.Done():
				return
			case task = <-c.high:
			case task = <-c.med:
			default:
				select {
				case <-ctx.Done():
					return
				case task = <-c.high:
				case task = <-c.med:
				case task = <-c.low:
				}
			}
		}

		if c.logger.Enabled(ctx, slog.LevelDebug) {
			c.logger.Debug("traffic controller: task dispatched", "task_id", task.id, "priority", task.priority)
		}
		c.executeTask(task)
	}
}

func (c *APITrafficController) executeTask(t *apiTask) {
	tracer := config.GetTracer()
	ctx, span := tracer.Start(t.ctx, "traffic_controller.execute",
		trace.WithAttributes(
			attribute.String("task_id", fmt.Sprintf("%d", t.id)),
			attribute.Int("priority", t.priority),
		),
	)
	defer span.End()

	// Rate limit guard
	c.mu.Lock()
	remaining := c.remainingRateLimit
	threshold := c.rateLimitThreshold
	c.mu.Unlock()

	if t.priority == PriorityEnrich && remaining < threshold {
		c.logger.Warn("traffic controller: skipping enrichment due to low quota", "task_id", t.id, "remaining", remaining)
		t.resp <- nil
		return
	}

	c.logger.Debug("traffic controller: executing task", "task_id", t.id, "priority", t.priority)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	msg := t.fn(ctx)
	t.resp <- msg
}

// UpdateRateLimit updates the internal tracking of the GitHub quota.
func (c *APITrafficController) UpdateRateLimit(remaining int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remainingRateLimit = remaining
	if c.logger.Enabled(context.Background(), slog.LevelDebug) {
		c.logger.Debug("traffic controller: updated rate limit", "remaining", remaining)
	}
}

// Remaining returns the last known remaining rate limit.
func (c *APITrafficController) Remaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remainingRateLimit
}

// Shutdown waits for the worker to finish processing.
func (c *APITrafficController) Shutdown() {
	c.wg.Wait()
}

// Submit wraps an API operation in a serialized, prioritized command.
func (c *APITrafficController) Submit(priority int, fn func(ctx context.Context) tea.Msg) tea.Cmd {
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
			ctx:      c.ctx, // Use root context for trace linkage and cancellation
		}

		if c.logger.Enabled(task.ctx, slog.LevelDebug) {
			c.logger.Debug("traffic controller: task submitted", "task_id", id, "priority", priority)
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
