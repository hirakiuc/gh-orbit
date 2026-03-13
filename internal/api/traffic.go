package api

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/types"
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
	userMu sync.Mutex // Serializes PriorityUser tasks to prevent out-of-order writes

	taskCounter uint64

	// Channels for prioritized tasks
	high chan *apiTask // Fast Track for PriorityUser
	med  chan *apiTask
	low  chan *apiTask

	// Rate Limit State
	rlInfo           atomic.Pointer[types.RateLimitInfo]
	lockoutUntil     atomic.Pointer[time.Time]

	// Workers
	workerLimit   int32
	done          chan struct{}

	rateLimitUpdates chan types.RateLimitInfo
}

func NewAPITrafficController(ctx context.Context, logger *slog.Logger) *APITrafficController {
	c := &APITrafficController{
		ctx:              ctx,
		logger:           logger,
		high:             make(chan *apiTask, 10),
		med:              make(chan *apiTask, 50),
		low:              make(chan *apiTask, 100),
		workerLimit:      3, // Default concurrency
		done:             make(chan struct{}),
		rateLimitUpdates: make(chan types.RateLimitInfo, 100),
	}

	// Initialize rate limit info with safe defaults
	c.rlInfo.Store(&types.RateLimitInfo{Remaining: 5000})

	// Start supervisor
	go c.supervisor()
	// Start background rate limit listener
	// #nosec G118: Background listener longevity tied to traffic controller done signal
	go c.rateLimitListener()

	return c
}

func (c *APITrafficController) Remaining() int {
	return c.rlInfo.Load().Remaining
}

func (c *APITrafficController) RateLimitUpdates() chan types.RateLimitInfo {
	return c.rateLimitUpdates
}

func (c *APITrafficController) Submit(priority int, fn types.TaskFunc) tea.Cmd {
	return func() tea.Msg {
		task := &apiTask{
			id:       atomic.AddUint64(&c.taskCounter, 1),
			priority: priority,
			fn:       fn,
			resp:     make(chan tea.Msg, 1),
			ctx:      c.ctx,
		}

		select {
		case <-c.done:
			return nil
		default:
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

func (c *APITrafficController) UpdateRateLimit(ctx context.Context, info types.RateLimitInfo) {
	c.rlInfo.Store(&info)

	// Update worker limits based on remaining quota
	newLimit := int32(3)
	if info.Remaining < 500 {
		newLimit = 1
		c.logger.InfoContext(ctx, "traffic controller: scaling down concurrency", "new_limit", newLimit)
	} else if info.Remaining > 1000 {
		newLimit = 3
		c.logger.InfoContext(ctx, "traffic controller: scaled up concurrency", "new_limit", newLimit)
	}
	atomic.StoreInt32(&c.workerLimit, newLimit)

	// Check for lockout (primary fallback)
	if info.Remaining == 0 && !info.Reset.IsZero() {
		c.lockoutUntil.Store(&info.Reset)
	}
}

func (c *APITrafficController) supervisor() {
	for {
		select {
		case <-c.done:
			return
		case <-time.After(10 * time.Millisecond):
			if atomic.LoadInt32(&activeWorkersCount) < atomic.LoadInt32(&c.workerLimit) {
				select {
				case t := <-c.high:
					go c.runTask(t)
				case t := <-c.med:
					go c.runTask(t)
				case t := <-c.low:
					go c.runTask(t)
				default:
				}
			}
		}
	}
}

var activeWorkersCount int32

func (c *APITrafficController) runTask(t *apiTask) {
	atomic.AddInt32(&activeWorkersCount, 1)
	defer atomic.AddInt32(&activeWorkersCount, -1)

	// Check lockout
	if until := c.lockoutUntil.Load(); until != nil && time.Now().Before(*until) {
		if t.priority != PriorityUser {
			c.logger.WarnContext(t.ctx, "traffic controller: lockout active, skipping background task", "task_id", t.id)
			t.resp <- nil
			return
		}
	}

	// Dynamic Throttling: Skip low-priority enrichment if quota is critical
	if t.priority == PriorityEnrich && c.Remaining() < 500 {
		c.logger.WarnContext(t.ctx, "traffic controller: skipping enrichment due to low quota", 
			"task_id", t.id, "remaining", c.Remaining(), "threshold", 500)
		t.resp <- nil
		return
	}

	// Execution
	if t.priority == PriorityUser {
		c.userMu.Lock()
		defer c.userMu.Unlock()
	}

	t.resp <- t.fn(t.ctx)
}

func (c *APITrafficController) rateLimitListener() {
	for {
		select {
		case <-c.done:
			// Drain remaining updates if any during shutdown
			c.logger.DebugContext(context.Background(), "traffic controller: rate limit listener stopping")
			return
		case info, ok := <-c.rateLimitUpdates:
			if !ok {
				return
			}
			c.UpdateRateLimit(c.ctx, info)
		}
	}
}

func (c *APITrafficController) Shutdown(ctx context.Context) {
	close(c.done)
	// Ensure listener goroutine stops
	close(c.rateLimitUpdates)
	c.logger.DebugContext(ctx, "traffic controller shutdown complete")
}
