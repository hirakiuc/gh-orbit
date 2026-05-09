package api

import (
	"context"
	"errors"
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

var ErrTrafficQueueFull = errors.New("traffic controller queue full")

type apiTask struct {
	id       uint64
	priority int
	fn       types.TaskFunc
	resp     chan any
	ctx      context.Context
	cleanup  func()
}

// APITrafficController ensures prioritized access to the GitHub API.
type APITrafficController struct {
	ctx     context.Context // Controller lifetime context
	cancel  context.CancelFunc
	logger  *slog.Logger
	userMu  sync.Mutex   // Serializes PriorityUser tasks to prevent out-of-order writes
	stateMu sync.RWMutex // Prevents new submissions from racing with shutdown admission cutoff

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

	submitBeforeLockHook func()
}

func NewAPITrafficController(ctx context.Context, logger *slog.Logger) *APITrafficController {
	controllerCtx, cancel := context.WithCancel(ctx)
	c := &APITrafficController{
		ctx:              controllerCtx,
		cancel:           cancel,
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

func (c *APITrafficController) ReportRateLimit(info models.RateLimitInfo) {
	select {
	case <-c.done:
		// Drop late updates after shutdown starts.
	case c.rateLimitUpdates <- info:
	default:
		// Drop update if channel is full.
	}
}

func (c *APITrafficController) Submit(ctx context.Context, priority int, fn types.TaskFunc) (<-chan any, error) {
	taskCtx, cleanup := c.composeTaskContext(ctx)
	task := &apiTask{
		id:       atomic.AddUint64(&c.taskCounter, 1),
		priority: priority,
		fn:       fn,
		resp:     make(chan any, 1),
		ctx:      taskCtx,
		cleanup:  cleanup,
	}

	if c.submitBeforeLockHook != nil {
		c.submitBeforeLockHook()
	}

	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	select {
	case <-c.done:
		task.cleanup()
		return nil, fmt.Errorf("traffic controller shutdown")
	default:
	}

	if taskCtx.Err() != nil {
		task.cleanup()
		task.resp <- nil
		return task.resp, nil
	}

	queue := c.queueForPriority(priority)
	select {
	case <-taskCtx.Done():
		task.cleanup()
		task.resp <- nil
		return task.resp, nil
	case queue <- task:
		return task.resp, nil
	default:
		task.cleanup()
		return nil, ErrTrafficQueueFull
	}
}

func (c *APITrafficController) composeTaskContext(requestCtx context.Context) (context.Context, func()) {
	if requestCtx == nil {
		requestCtx = context.Background()
	}

	var (
		taskCtx context.Context
		cancel  context.CancelFunc
	)
	if deadline, ok := requestCtx.Deadline(); ok {
		taskCtx, cancel = context.WithDeadline(c.ctx, deadline)
	} else {
		taskCtx, cancel = context.WithCancel(c.ctx)
	}

	stop := context.AfterFunc(requestCtx, cancel)
	if requestCtx.Err() != nil {
		cancel()
	}
	return taskCtx, func() {
		stop()
		cancel()
	}
}

func (c *APITrafficController) queueForPriority(priority int) chan *apiTask {
	switch priority {
	case PriorityUser:
		return c.high
	case PrioritySync:
		return c.med
	default:
		return c.low
	}
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
	} else if info.Remaining > 0 {
		c.lockoutUntil.Store(nil)
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
	defer t.cleanup()

	if t.ctx.Err() != nil {
		t.resp <- nil
		return
	}

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
	c.stateMu.Lock()
	c.cancel()
	close(c.done)

	// Drain remaining tasks to unblock any callers
	c.drainQueue(c.high)
	c.drainQueue(c.med)
	c.drainQueue(c.low)
	c.stateMu.Unlock()

	// Wait for all background goroutines (supervisor, listener, and active workers)
	c.workerWG.Wait()
	c.logger.DebugContext(ctx, "traffic controller shutdown complete")
}

func (c *APITrafficController) drainQueue(q chan *apiTask) {
	for {
		select {
		case t := <-q:
			t.cleanup()
			t.resp <- nil
		default:
			return
		}
	}
}
