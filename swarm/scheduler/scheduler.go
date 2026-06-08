// Package scheduler provides event-driven agent execution for the swarm.
//
// It listens for events on the EventBus and spawns triggered agents
// concurrently, respecting a max concurrency limit (critical for local LLMs
// like Ollama that share VRAM).
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
)

// Event represents a trigger event in the swarm.
type Event struct {
	Type    string // e.g., "FILE_MODIFIED", "BUILD_FAILED", "TASK_COMPLETED"
	Source  string // originator agent or system name
	Content string // payload

	// ChainDepth tracks how many cascading triggers have occurred.
	// Used as a circuit breaker to prevent infinite feedback loops.
	ChainDepth    int
	MaxChainDepth int // 0 means use default (5)
}

// DefaultMaxChainDepth is the maximum cascading trigger depth.
const DefaultMaxChainDepth = 5

// CanCascade returns true if this event can trigger further events.
func (e Event) CanCascade() bool {
	max := e.MaxChainDepth
	if max <= 0 {
		max = DefaultMaxChainDepth
	}
	return e.ChainDepth < max
}

// Child creates a derived event with incremented chain depth.
func (e Event) Child(eventType, source, content string) Event {
	return Event{
		Type:          eventType,
		Source:        source,
		Content:       content,
		ChainDepth:    e.ChainDepth + 1,
		MaxChainDepth: e.MaxChainDepth,
	}
}

// EventBus is a pub/sub channel for swarm events.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]chan Event // event type → subscriber channels
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string][]chan Event),
	}
}

// Subscribe returns a channel that receives events of the given type.
func (eb *EventBus) Subscribe(eventType string) <-chan Event {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	ch := make(chan Event, 16)
	eb.subscribers[eventType] = append(eb.subscribers[eventType], ch)
	return ch
}

// Publish sends an event to all subscribers of that event type.
// Also sends to "*" (wildcard) subscribers.
func (eb *EventBus) Publish(evt Event) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for _, ch := range eb.subscribers[evt.Type] {
		select {
		case ch <- evt:
		default:
		}
	}
	for _, ch := range eb.subscribers["*"] {
		select {
		case ch <- evt:
		default:
		}
	}
}

// Close shuts down all subscriber channels.
func (eb *EventBus) Close() {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	for eventType, channels := range eb.subscribers {
		for _, ch := range channels {
			close(ch)
		}
		delete(eb.subscribers, eventType)
	}
}

// --- AgentRunner ---

// AgentRunner wraps a v2 agent with its trigger types.
type AgentRunner struct {
	Agent    agent.Agent
	Triggers []string
	// Timeout is the max duration for a triggered agent run. 0 means no timeout.
	Timeout  time.Duration
}

// --- Scheduler ---

// Scheduler manages a pool of workers to execute event-triggered agents concurrently.
type Scheduler struct {
	maxWorkers     int
	sem            chan struct{}
	bus            *EventBus
	agents         []AgentRunner
	sessionSvc     session.Service
	defaultTimeout time.Duration
	onDispatch     func(string, string, int)
	onComplete     func(string, string, time.Duration, error)
}

// Config for the scheduler.
type Config struct {
	MaxWorkers   int
	Bus          *EventBus
	Agents       []AgentRunner
	SessionSvc   session.Service
	// DefaultTimeout applies to agents without a specific timeout. 0 = no timeout.
	DefaultTimeout time.Duration
	// OnDispatch is called when an agent is dispatched (observability hook).
	OnDispatch func(agentName, triggerType string, chainDepth int)
	// OnComplete is called when a dispatched agent finishes (observability hook).
	OnComplete func(agentName, triggerType string, duration time.Duration, err error)
}

// New creates a scheduler.
func New(cfg Config) *Scheduler {
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 2
	}
	sessSvc := cfg.SessionSvc
	if sessSvc == nil {
		sessSvc = session.InMemoryService()
	}
	return &Scheduler{
		maxWorkers:     cfg.MaxWorkers,
		sem:            make(chan struct{}, cfg.MaxWorkers),
		bus:            cfg.Bus,
		agents:         cfg.Agents,
		sessionSvc:     sessSvc,
		defaultTimeout: cfg.DefaultTimeout,
		onDispatch:     cfg.OnDispatch,
		onComplete:     cfg.OnComplete,
	}
}

// Start begins the event loop. Runs in background goroutine.
// Cancel the context to stop the scheduler.
func (s *Scheduler) Start(ctx context.Context) {
	// Build trigger → agents map
	triggerMap := make(map[string][]AgentRunner)
	for _, ar := range s.agents {
		for _, trigger := range ar.Triggers {
			triggerMap[trigger] = append(triggerMap[trigger], ar)
		}
	}

	// Subscribe to all trigger types
	type sub struct {
		triggerType string
		ch          <-chan Event
	}
	var subs []sub
	for tt := range triggerMap {
		subs = append(subs, sub{triggerType: tt, ch: s.bus.Subscribe(tt)})
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			dispatched := false
			for _, s2 := range subs {
				select {
				case evt, ok := <-s2.ch:
					if !ok {
						continue
					}
					for _, ar := range triggerMap[s2.triggerType] {
						s.dispatch(ctx, ar, evt)
					}
					dispatched = true
				default:
				}
			}

			if !dispatched {
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

func (s *Scheduler) dispatch(ctx context.Context, ar AgentRunner, evt Event) {
	// Circuit breaker: prevent infinite cascading loops
	if !evt.CanCascade() {
		fmt.Printf("[Scheduler] Circuit breaker: chain depth %d exceeded for %s (trigger: %s)\n",
			evt.ChainDepth, ar.Agent.Name(), evt.Type)
		return
	}

	go func() {
		// Acquire semaphore slot
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-s.sem }()

		// Small delay to let the triggering pipeline advance
		time.Sleep(500 * time.Millisecond)

		// Observability: dispatch hook
		if s.onDispatch != nil {
			s.onDispatch(ar.Agent.Name(), evt.Type, evt.ChainDepth)
		}
		start := time.Now()

		fmt.Printf("[Scheduler] Spawning triggered agent: %s (Trigger: %s, Depth: %d)\n",
			ar.Agent.Name(), evt.Type, evt.ChainDepth)

		// Apply timeout
		runCtx := ctx
		timeout := ar.Timeout
		if timeout <= 0 {
			timeout = s.defaultTimeout
		}
		var cancel context.CancelFunc
		if timeout > 0 {
			runCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		// Create a session for the triggered run
		resp, err := s.sessionSvc.Create(runCtx, &session.CreateRequest{
			AppName: ar.Agent.Name(),
			UserID:  "scheduler",
			State: map[string]any{
				"trigger_event":   evt.Type,
				"trigger_content": evt.Content,
				"trigger_source":  evt.Source,
				"prompt":          evt.Content,
			},
		})
		if err != nil {
			fmt.Printf("[Scheduler] Failed to create session for %s: %v\n", ar.Agent.Name(), err)
			if s.onComplete != nil {
				s.onComplete(ar.Agent.Name(), evt.Type, time.Since(start), err)
			}
			return
		}

		invCtx := agent.NewInvocationContext(agent.InvocationContextParams{
			Ctx:     runCtx,
			Agent:   ar.Agent,
			Session: resp.Session,
		})

		// Consume the Run iterator to drive execution
		var runErr error
		for evt, err := range ar.Agent.Run(invCtx) {
			if err != nil {
				runErr = err
				fmt.Printf("[Scheduler] Triggered agent %s error: %v\n", ar.Agent.Name(), err)
				break
			}
			_ = evt
		}

		// Observability: complete hook
		duration := time.Since(start)
		if s.onComplete != nil {
			s.onComplete(ar.Agent.Name(), evt.Type, duration, runErr)
		}
		fmt.Printf("[Scheduler] Agent %s completed in %s\n", ar.Agent.Name(), duration)
	}()
}
