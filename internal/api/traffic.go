package api

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hirakiuc/gh-orbit/internal/models"
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
	resp     chan any
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
	rlInfo       atomic.Pointer[models.RateLimitInfo]
	lockoutUntil atomic.Pointer[time.Time]

	// Workers
	workerLimit        int32
	activeWorkersCount int32
	done               chan struct{}
	workerWG           sync.WaitGroup

	rateLimitUpdates chan models.RateLimitInfo
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
		rateLimitUpdates: make(chan models.RateLimitInfo, 100),
	}

	// Initialize rate limit info with safe defaults
	c.rlInfo.Store(&models.RateLimitInfo{Remaining: 5000})

	// Start background routines
	c.workerWG.Add(2)
	go func() {
		defer c.workerWG.Done()
		c.supervisor()
	}()
	go func() {
		defer c.workerWG.Done()
		c.rateLimitListener()
	}()

	return c
}

func (c *APITrafficController) Remaining() int {
	return c.rlInfo.Load().Remaining
}

func (c *APITrafficController) RateLimitUpdates() chan models.RateLimitInfo {
	return c.rateLimitUpdates
}

func (c *APITrafficController) Submit(priority int, fn types.TaskFunc) (<-chan any, error) {
	task := &apiTask{
		id:       atomic.AddUint64(&c.taskCounter, 1),
		priority: priority,
		fn:       fn,
		resp:     make(chan any, 1),
		ctx:      c.ctx,
	}

	select {
	case <-c.done:
		return nil, fmt.Errorf("traffic controller shutdown")
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

	return task.resp, nil
}

func (c *APITrafficController) UpdateRateLimit(ctx context.Context, info models.RateLimitInfo) {
	c.rlInfo.Store(&info)

	// Update worker limits based on remaining quota
	newLimit := int32(3)
	if info.Remaining < 500 {
		newLimit = 1
	}

	oldLimit := atomic.LoadInt32(&c.workerLimit)
	if newLimit != oldLimit {
		if newLimit < oldLimit {
			c.logger.DebugContext(ctx, "traffic controller: scaling down concurrency", "new_limit", newLimit)
		} else {
			c.logger.DebugContext(ctx, "traffic controller: scaled up concurrency", "new_limit", newLimit)
		}
		atomic.StoreInt32(&c.workerLimit, newLimit)
	}

	// Check for lockout (primary fallback)
	if info.Remaining == 0 && !info.Reset.IsZero() {
		c.lockoutUntil.Store(&info.Reset)
	}
}

func (c *APITrafficController) supervisor() {
	// We use a ticker to periodically check for workers becoming available
	// while avoiding tight loop spinning.
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		default:
			if atomic.LoadInt32(&c.activeWorkersCount) >= atomic.LoadInt32(&c.workerLimit) {
				// At worker limit, wait for a worker to finish.
				select {
				case <-c.done:
					return
				case <-ticker.C:
				}
				continue
			}

			if t := c.dequeueTask(); t != nil {
				c.spawnTask(t)
				continue
			}

			select {
			case <-c.done:
				return
			case <-ticker.C:
			}
		}
	}
}

func (c *APITrafficController) dequeueTask() *apiTask {
	select {
	case t := <-c.high:
		return t
	default:
	}

	select {
	case t := <-c.med:
		return t
	default:
	}

	select {
	case t := <-c.low:
		return t
	default:
	}

	return nil
}

func (c *APITrafficController) spawnTask(t *apiTask) {
	atomic.AddInt32(&c.activeWorkersCount, 1)
	c.workerWG.Add(1)
	go func() {
		defer c.workerWG.Done()
		c.runTask(t)
	}()
}

func (c *APITrafficController) runTask(t *apiTask) {
	defer atomic.AddInt32(&c.activeWorkersCount, -1)

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

	// Drain remaining tasks to unblock any callers
	c.drainQueue(c.high)
	c.drainQueue(c.med)
	c.drainQueue(c.low)

	// Wait for all background goroutines (supervisor, listener, and active workers)
	c.workerWG.Wait()
	c.logger.DebugContext(ctx, "traffic controller shutdown complete")
}

func (c *APITrafficController) drainQueue(q chan *apiTask) {
	for {
		select {
		case t := <-q:
			t.resp <- nil
		default:
			return
		}
	}
}
