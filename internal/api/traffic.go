package api

import (
	"context"
	"log/slog"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Priority levels
const (
	PriorityEnrich = iota
	PrioritySync
	PriorityUser
)

type apiTask struct {
	priority int
	fn       func(ctx context.Context) tea.Msg
	resp     chan tea.Msg
}

// APITrafficController ensures serialized, prioritized access to the GitHub API.
type APITrafficController struct {
	logger *slog.Logger
	mu     sync.Mutex

	// Channels for prioritized tasks
	high chan *apiTask
	med  chan *apiTask
	low  chan *apiTask

	// Threshold for pausing background tasks (Enrichment)
	rateLimitThreshold int
	remainingRateLimit int
}

func NewAPITrafficController(logger *slog.Logger) *APITrafficController {
	tc := &APITrafficController{
		logger:             logger,
		high:               make(chan *apiTask),
		med:                make(chan *apiTask),
		low:                make(chan *apiTask),
		rateLimitThreshold: 500,
		remainingRateLimit: 5000,
	}
	go tc.worker()
	return tc
}

func (c *APITrafficController) worker() {
	for {
		var task *apiTask

		// Nested select for genuine preemption
		select {
		case task = <-c.high:
		default:
			select {
			case task = <-c.high:
			case task = <-c.med:
			default:
				select {
				case task = <-c.high:
				case task = <-c.med:
				case task = <-c.low:
				}
			}
		}

		c.executeTask(task)
	}
}

func (c *APITrafficController) executeTask(t *apiTask) {
	// Rate limit guard
	c.mu.Lock()
	remaining := c.remainingRateLimit
	threshold := c.rateLimitThreshold
	c.mu.Unlock()

	if t.priority == PriorityEnrich && remaining < threshold {
		c.logger.Warn("traffic controller: skipping enrichment due to low quota", "remaining", remaining)
		t.resp <- nil
		return
	}

	c.logger.Debug("traffic controller: executing task", "priority", t.priority)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msg := t.fn(ctx)
	t.resp <- msg
}

// UpdateRateLimit updates the internal tracking of the GitHub quota.
func (c *APITrafficController) UpdateRateLimit(remaining int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remainingRateLimit = remaining
	c.logger.Debug("traffic controller: updated rate limit", "remaining", remaining)
}

// Remaining returns the last known remaining rate limit.
func (c *APITrafficController) Remaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remainingRateLimit
}

// Submit wraps an API operation in a serialized, prioritized command.
func (c *APITrafficController) Submit(priority int, fn func(ctx context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		task := &apiTask{
			priority: priority,
			fn:       fn,
			resp:     make(chan tea.Msg, 1),
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
