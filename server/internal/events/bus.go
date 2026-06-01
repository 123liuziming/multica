package events

import (
	"log/slog"
	"sync"
	"time"
)

// Event represents a domain event published by handlers or services.
type Event struct {
	Type        string // e.g. "issue:created", "inbox:new"
	WorkspaceID string // routes to correct Hub room
	ActorType   string // "member", "agent", or "system"
	ActorID     string
	Payload     any // JSON-serializable, same shape as current WS payloads

	// Optional scope hints used by the realtime fanout layer to route the
	// event to a more specific scope than `workspace:{WorkspaceID}`. When set
	// these tell the listener which Redis stream / Hub room to publish on
	// without re-deserializing Payload. See MUL-1138 phase 1.
	TaskID        string
	ChatSessionID string
}

// Handler is a function that processes an event.
type Handler func(Event)

// Bus is an in-process synchronous pub/sub event bus.
type Bus struct {
	mu             sync.RWMutex
	listeners      map[string][]Handler
	globalHandlers []Handler
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{
		listeners: make(map[string][]Handler),
	}
}

// Subscribe registers a handler for a given event type.
// Handlers are called synchronously in registration order.
func (b *Bus) Subscribe(eventType string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[eventType] = append(b.listeners[eventType], h)
}

// SubscribeAll registers a handler that receives ALL events regardless of type.
// Global handlers are called after type-specific handlers.
func (b *Bus) SubscribeAll(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.globalHandlers = append(b.globalHandlers, h)
}

// Publish dispatches an event to all registered handlers for that event type.
// Type-specific handlers run first, then global (SubscribeAll) handlers.
// Each handler is called synchronously. Panics in individual handlers are
// recovered so one failing handler does not prevent others from executing.
func (b *Bus) Publish(e Event) {
	start := time.Now()
	b.mu.RLock()
	handlers := b.listeners[e.Type]
	globals := b.globalHandlers
	b.mu.RUnlock()

	for i, h := range handlers {
		func() {
			t0 := time.Now()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in event listener", "event_type", e.Type, "recovered", r)
				}
				if d := time.Since(t0); d > 50*time.Millisecond {
					slog.Warn("slow event handler", "event_type", e.Type, "handler_index", i, "duration_ms", d.Milliseconds())
				}
			}()
			h(e)
		}()
	}

	for i, h := range globals {
		func() {
			t0 := time.Now()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in global event listener", "event_type", e.Type, "recovered", r)
				}
				if d := time.Since(t0); d > 50*time.Millisecond {
					slog.Warn("slow global event handler", "event_type", e.Type, "handler_index", i, "duration_ms", d.Milliseconds())
				}
			}()
			h(e)
		}()
	}

	if d := time.Since(start); d > 100*time.Millisecond {
		slog.Warn("slow event publish", "event_type", e.Type, "total_duration_ms", d.Milliseconds(), "handler_count", len(handlers)+len(globals))
	}
}
