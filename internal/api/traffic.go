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
	batch    *notificationBatchPlan
}

type taskPolicyDecision int

const (
	taskPolicyProceed taskPolicyDecision = iota
	taskPolicyCanceled
	taskPolicySkipLockout
	taskPolicySkipLowQuota
)

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
	userTaskActive     int32
	done               chan struct{}
	workerWG           sync.WaitGroup

	submitBeforeLockHook func()
}

func NewAPITrafficController(ctx context.Context, logger *slog.Logger) *APITrafficController {
	controllerCtx, cancel := context.WithCancel(ctx)
	c := &APITrafficController{
		ctx:         controllerCtx,
		cancel:      cancel,
		logger:      logger,
		high:        make(chan *apiTask, 10),
		med:         make(chan *apiTask, 50),
		low:         make(chan *apiTask, 100),
		workerLimit: 3, // Default concurrency
		done:        make(chan struct{}),
	}

	// Initialize rate limit info with safe defaults
	c.rlInfo.Store(&models.RateLimitInfo{Remaining: 5000})

	// Start background supervisor
	c.workerWG.Add(1)
	go func() {
		defer c.workerWG.Done()
		c.supervisor()
	}()

	return c
}

func (c *APITrafficController) Remaining() int {
	return c.rlInfo.Load().Remaining
}

func (c *APITrafficController) ReportRateLimit(info models.RateLimitInfo) {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()

	if c.ctx.Err() != nil {
		// Drop late updates after shutdown starts.
		return
	}

	c.UpdateRateLimit(c.ctx, info)
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
	// Keep the coordinator polling-based for now. The fixed 2ms ticker lets
	// worker completion and rate-limit changes become visible without adding a
	// second wakeup protocol between Submit, workers, and shutdown. Revisit this
	// only if profiling shows measurable idle scheduler cost or if coordination
	// bugs appear that need causal wakeups instead of periodic polling.
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		default:
			if atomic.LoadInt32(&c.userTaskActive) == 0 {
				if t := c.dequeueUserTask(); t != nil {
					atomic.StoreInt32(&c.userTaskActive, 1)
					c.spawnUserTask(t)
					continue
				}
			}

			// User work has priority and owns serialization before capacity. Do
			// not let later work consume a permit that the active user task or
			// notification batch children need.
			if atomic.LoadInt32(&c.userTaskActive) != 0 || !c.tryAcquireCapacity() {
				select {
				case <-c.done:
					return
				case <-ticker.C:
				}
				continue
			}

			if t := c.dequeueBackgroundTask(); t != nil {
				c.spawnReservedTask(t)
				continue
			}
			c.releaseCapacity()

			select {
			case <-c.done:
				return
			case <-ticker.C:
			}
		}
	}
}

func (c *APITrafficController) dequeueUserTask() *apiTask {
	select {
	case t := <-c.high:
		return t
	default:
	}
	return nil
}

func (c *APITrafficController) dequeueBackgroundTask() *apiTask {
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

func (c *APITrafficController) spawnReservedTask(t *apiTask) {
	c.workerWG.Add(1)
	go func() {
		defer c.workerWG.Done()
		defer c.releaseCapacity()
		c.runBackgroundTask(t)
	}()
}

func (c *APITrafficController) spawnUserTask(t *apiTask) {
	c.workerWG.Add(1)
	go func() {
		defer c.workerWG.Done()
		defer atomic.StoreInt32(&c.userTaskActive, 0)
		c.runUserTask(t)
	}()
}

func (c *APITrafficController) runBackgroundTask(t *apiTask) {
	defer t.cleanup()

	switch c.evaluateTaskPolicy(t) {
	case taskPolicyProceed:
		c.executeTask(t)
	default:
		t.resp <- nil
		return
	}
}

func (c *APITrafficController) runUserTask(t *apiTask) {
	defer t.cleanup()

	if c.evaluateTaskPolicy(t) != taskPolicyProceed {
		t.resp <- nil
		return
	}

	// User serialization is always acquired before global API capacity. Batch
	// children run while this lock is held but acquire capacity internally.
	c.userMu.Lock()
	defer c.userMu.Unlock()

	if t.batch != nil {
		t.resp <- c.executeNotificationBatch(t.ctx, *t.batch)
		return
	}
	if !c.acquireCapacity(t.ctx) {
		t.resp <- nil
		return
	}
	defer c.releaseCapacity()
	t.resp <- t.fn(t.ctx)
}

func (c *APITrafficController) taskCanceledBeforeExecution(t *apiTask) bool {
	return t.ctx.Err() != nil
}

func (c *APITrafficController) evaluateTaskPolicy(t *apiTask) taskPolicyDecision {
	if c.taskCanceledBeforeExecution(t) {
		return taskPolicyCanceled
	}
	if c.shouldSkipTaskDuringLockout(t) {
		return taskPolicySkipLockout
	}
	if c.shouldSkipEnrichmentDueToLowQuota(t) {
		return taskPolicySkipLowQuota
	}
	return taskPolicyProceed
}

func (c *APITrafficController) shouldSkipTaskDuringLockout(t *apiTask) bool {
	until := c.lockoutUntil.Load()
	if until == nil || !time.Now().Before(*until) || c.requiresSerializedUserExecution(t) {
		return false
	}

	c.logger.WarnContext(t.ctx, "traffic controller: lockout active, skipping background task", "task_id", t.id)
	return true
}

func (c *APITrafficController) shouldSkipEnrichmentDueToLowQuota(t *apiTask) bool {
	if t.priority != PriorityEnrich {
		return false
	}

	remaining := c.Remaining()
	if remaining >= 500 {
		return false
	}

	c.logger.WarnContext(t.ctx, "traffic controller: skipping enrichment due to low quota",
		"task_id", t.id, "remaining", remaining, "threshold", 500)
	return true
}

func (c *APITrafficController) requiresSerializedUserExecution(t *apiTask) bool {
	return t.priority == PriorityUser
}

func (c *APITrafficController) executeTask(t *apiTask) {
	t.resp <- t.fn(t.ctx)
}

func (c *APITrafficController) tryAcquireCapacity() bool {
	for {
		active := atomic.LoadInt32(&c.activeWorkersCount)
		if active >= atomic.LoadInt32(&c.workerLimit) {
			return false
		}
		if atomic.CompareAndSwapInt32(&c.activeWorkersCount, active, active+1) {
			return true
		}
	}
}

func (c *APITrafficController) acquireCapacity(ctx context.Context) bool {
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		if c.tryAcquireCapacity() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-c.done:
			return false
		case <-ticker.C:
		}
	}
}

func (c *APITrafficController) releaseCapacity() {
	atomic.AddInt32(&c.activeWorkersCount, -1)
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
