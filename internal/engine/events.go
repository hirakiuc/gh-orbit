package engine

import (
	"sync"
)

// EngineEvent represents a mutation or significant state change in the engine.
type EngineEvent string

const (
	EventNotificationsChanged EngineEvent = "notifications_changed"
	EventEnrichmentUpdated    EngineEvent = "enrichment_updated"
)

// EventBus handles internal pub/sub for engine events.
type EventBus struct {
	subscribers map[EngineEvent][]chan struct{}
	mu          sync.RWMutex
}

func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[EngineEvent][]chan struct{}),
	}
}

// Subscribe returns a channel that receives a signal when the event occurs.
func (b *EventBus) Subscribe(event EngineEvent) <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan struct{}, 1)
	b.subscribers[event] = append(b.subscribers[event], ch)
	return ch
}

// Publish signals all subscribers of an event.
func (b *EventBus) Publish(event EngineEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subscribers[event] {
		select {
		case ch <- struct{}{}:
		default:
			// Buffer full, skip to avoid blocking publishers
		}
	}
}
