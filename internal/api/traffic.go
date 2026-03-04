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

// APITrafficController ensures serialized, prioritized access to the GitHub API.
type APITrafficController struct {
	logger *slog.Logger
	mu     sync.Mutex
	
	// Threshold for pausing background tasks (Enrichment)
	rateLimitThreshold int
	remainingRateLimit int
}

func NewAPITrafficController(logger *slog.Logger) *APITrafficController {
	return &APITrafficController{
		logger:             logger,
		rateLimitThreshold: 500,
		remainingRateLimit: 5000, // Default assume healthy
	}
}

// UpdateRateLimit updates the internal tracking of the GitHub quota.
func (c *APITrafficController) UpdateRateLimit(remaining int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remainingRateLimit = remaining
	c.logger.Debug("traffic controller: updated rate limit", "remaining", remaining)
}

// Submit wraps an API operation in a serialized command.
func (c *APITrafficController) Submit(priority int, fn func(ctx context.Context) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		// Serialization lock: prevents overlapping API operations
		c.mu.Lock()
		defer c.mu.Unlock()

		// Rate limit guard: skip enrichment if quota is low
		if priority == PriorityEnrich && c.remainingRateLimit < c.rateLimitThreshold {
			c.logger.Warn("traffic controller: skipping enrichment due to low quota", "remaining", c.remainingRateLimit)
			return nil
		}

		c.logger.Debug("traffic controller: executing task", "priority", priority)
		
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		return fn(ctx)
	}
}
