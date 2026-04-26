package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
)

func TestEventBus_Stress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bus := NewEventBus()
		const numSubscribers = 100
		const numPublishers = 50
		const numEvents = 200

		ctx, cancel := context.WithCancel(context.Background())
		var totalReceived int64
		var wg sync.WaitGroup

		// 1. Setup many concurrent subscribers
		for i := 0; i < numSubscribers; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				ch := bus.Subscribe(EventNotificationsChanged)

				for {
					select {
					case <-ch:
						atomic.AddInt64(&totalReceived, 1)
					case <-ctx.Done():
						return
					}
				}
			}(i)
		}

		// Wait for subscribers to settle
		synctest.Wait()

		// 2. Setup many concurrent publishers
		var pubWG sync.WaitGroup
		pubWG.Add(numPublishers)
		for i := 0; i < numPublishers; i++ {
			go func(id int) {
				defer pubWG.Done()
				for j := 0; j < numEvents; j++ {
					bus.Publish(EventNotificationsChanged)
					if j%10 == 0 {
						// Mix in some random subs during active publishing
						bus.Subscribe(EventEnrichmentUpdated)
					}
				}
			}(i)
		}

		pubWG.Wait()
		// Wait for events to propagate
		synctest.Wait()

		cancel()
		wg.Wait()

		// 3. Final verification
		received := atomic.LoadInt64(&totalReceived)
		t.Logf("Stress Test complete. Total events received: %d", received)
		assert.Greater(t, received, int64(0), "Subscribers should have received at least some events")
	})
}

func TestEventBus_DeadlockDetection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bus := NewEventBus()
		const iterations = 1000

		var wg sync.WaitGroup
		wg.Add(2)

		// Simulate high contention between lock types
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				bus.Subscribe(EventNotificationsChanged)
			}
		}()

		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				bus.Publish(EventNotificationsChanged)
			}
		}()

		wg.Wait()
	})
}

func TestEventBus_BufferSaturation(t *testing.T) {
	bus := NewEventBus()
	// Channel is buffered at size 1
	ch := bus.Subscribe(EventNotificationsChanged)

	// Publish twice without reading
	bus.Publish(EventNotificationsChanged)
	bus.Publish(EventNotificationsChanged)

	// Should not block and should have one item in buffer
	select {
	case <-ch:
		// success
	default:
		t.Fatal("Should have received event from buffer")
	}

	// Second publish was dropped due to saturation (non-blocking)
	select {
	case <-ch:
		t.Fatal("Buffer should have been empty")
	default:
		// success
	}
}

func BenchmarkEventBus_Publish(b *testing.B) {
	bus := NewEventBus()
	for i := 0; i < 10; i++ {
		bus.Subscribe(EventNotificationsChanged)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(EventNotificationsChanged)
	}
}
