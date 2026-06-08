package agent

import "sync"

// EventType identifies the kind of real-time event emitted during agent execution.
// This is a go-brain exclusive feature for streaming to UIs via WebSocket/SSE.
type EventType string

const (
	EventRunStarted   EventType = "run.started"
	EventRunCompleted EventType = "run.completed"
	EventChunk        EventType = "chunk"
	EventToolCall     EventType = "tool.call"
	EventToolResult   EventType = "tool.result"
	EventAgentSwitch  EventType = "agent.switch"
	EventHITLPause    EventType = "hitl.pause"
	EventHITLResume   EventType = "hitl.resume"
	EventError        EventType = "error"
)

// BusEvent is an event emitted to the event bus for UI consumers.
type BusEvent struct {
	Type    EventType `json:"event"`
	Agent   string    `json:"agent,omitempty"`
	Payload any       `json:"payload,omitempty"`
}

// EventHandler processes events from the event bus.
type EventHandler func(BusEvent)

// EventBus allows library consumers (HTTP/WebSocket servers, UIs) to listen
// to the agent's real-time execution stream. Thread-safe.
type EventBus struct {
	mu       sync.RWMutex
	handlers []EventHandler
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{}
}

// On registers a handler to receive events.
func (b *EventBus) On(handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, handler)
}

// Emit sends an event to all registered handlers.
func (b *EventBus) Emit(evt BusEvent) {
	b.mu.RLock()
	handlers := make([]EventHandler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	for _, h := range handlers {
		h(evt)
	}
}
