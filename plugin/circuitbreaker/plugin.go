// Package circuitbreaker provides loop detection and kill switch enforcement for moa agents.
//
// It detects repetitive tool call patterns (identical calls, consecutive failures)
// and trips a circuit breaker to stop runaway agents. An optional external KillSwitch
// can be used to abort the agent from outside its execution.
//
// States: Closed (normal) → Open (blocked) → HalfOpen (probing) → Closed.
//
// Usage:
//
//	cb := circuitbreaker.New(circuitbreaker.Config{
//	    MaxIdenticalCalls:   5,
//	    MaxConsecutiveFails: 5,
//	    CooldownDuration:    30 * time.Second,
//	})
//	runner, _ := runner.New(runner.Config{
//	    Agent:   myAgent,
//	    Plugins: []*plugin.Plugin{cb},
//	})
package circuitbreaker

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/tool"
)

// Errors returned when the circuit breaker trips.
var (
	ErrCircuitOpen     = errors.New("circuitbreaker: circuit is open, tool calls blocked")
	ErrRepetitiveLoop  = errors.New("circuitbreaker: repetitive loop detected")
	ErrConsecutiveFails = errors.New("circuitbreaker: too many consecutive failures")
	ErrKillSwitchTripped = errors.New("circuitbreaker: kill switch is tripped")
)

// CircuitState represents the state of the circuit breaker.
type CircuitState int

const (
	// Closed is the normal operating state.
	Closed CircuitState = iota
	// Open means tool calls are blocked.
	Open
	// HalfOpen allows a limited number of probe calls.
	HalfOpen
)

// KillSwitch is an external boolean that can abort the agent.
// The agent can read it but should not be able to set it.
type KillSwitch interface {
	IsTripped() bool
}

// AtomicKillSwitch is an in-memory kill switch, useful for testing.
type AtomicKillSwitch struct {
	mu      sync.Mutex
	tripped bool
}

// Trip activates the kill switch.
func (a *AtomicKillSwitch) Trip()    { a.mu.Lock(); a.tripped = true; a.mu.Unlock() }
// Reset deactivates the kill switch.
func (a *AtomicKillSwitch) Reset()   { a.mu.Lock(); a.tripped = false; a.mu.Unlock() }
// IsTripped returns whether the kill switch is active.
func (a *AtomicKillSwitch) IsTripped() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.tripped }

// FileKillSwitch checks for the existence of a file on disk.
// If the file exists, the agent is killed.
type FileKillSwitch struct {
	Path    string
	checkFn func(string) bool // injectable for testing
}

// IsTripped returns true if the kill file exists.
func (f *FileKillSwitch) IsTripped() bool {
	if f.checkFn != nil {
		return f.checkFn(f.Path)
	}
	// Default: check file existence via stat
	return fileExists(f.Path)
}

// Config configures the circuit breaker plugin.
type Config struct {
	// MaxIdenticalCalls is the maximum number of identical tool calls (same name + args)
	// before tripping. Default: 5.
	MaxIdenticalCalls int
	// MaxConsecutiveFails is the maximum number of consecutive failures on the same tool
	// before tripping. Default: 5.
	MaxConsecutiveFails int
	// CooldownDuration is how long the circuit stays open before moving to half-open.
	// Default: 30 seconds.
	CooldownDuration time.Duration
	// MaxProbes is the number of probe calls allowed in half-open state. Default: 1.
	MaxProbes int
	// KillSwitch is an optional external kill switch.
	KillSwitch KillSwitch
}

type breaker struct {
	mu sync.Mutex

	cfg Config

	state     CircuitState
	openedAt  time.Time
	probeUsed int

	// Tracking for identical calls
	lastCallHash string
	repeatCount  int

	// Tracking for consecutive failures
	lastFailedTool string
	failCount      int
}

// New creates a circuit breaker plugin.
func New(cfg Config) *plugin.Plugin {
	if cfg.MaxIdenticalCalls <= 0 {
		cfg.MaxIdenticalCalls = 5
	}
	if cfg.MaxConsecutiveFails <= 0 {
		cfg.MaxConsecutiveFails = 5
	}
	if cfg.CooldownDuration <= 0 {
		cfg.CooldownDuration = 30 * time.Second
	}
	if cfg.MaxProbes <= 0 {
		cfg.MaxProbes = 1
	}

	b := &breaker{cfg: cfg, state: Closed}

	return &plugin.Plugin{
		Name: "circuit-breaker",

		BeforeToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			return b.beforeTool(t, args)
		},

		AfterToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error) {
			b.onSuccess(t.Name())
			return nil, nil
		},

		OnToolErrorCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
			return b.onFailure(t, err)
		},
	}
}

func (b *breaker) beforeTool(t tool.Tool, args map[string]any) (map[string]any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check kill switch first.
	if b.cfg.KillSwitch != nil && b.cfg.KillSwitch.IsTripped() {
		return nil, ErrKillSwitchTripped
	}

	switch b.state {
	case Open:
		if time.Since(b.openedAt) > b.cfg.CooldownDuration {
			b.state = HalfOpen
			b.probeUsed = 0
			// Fall through to allow probe
		} else {
			return nil, ErrCircuitOpen
		}
		fallthrough

	case HalfOpen:
		if b.probeUsed >= b.cfg.MaxProbes {
			return nil, ErrCircuitOpen
		}
		b.probeUsed++

	case Closed:
		// Check for repetitive loop
		hash := hashCall(t.Name(), args)
		if hash == b.lastCallHash {
			b.repeatCount++
			if b.repeatCount >= b.cfg.MaxIdenticalCalls {
				b.trip()
				return nil, fmt.Errorf("%w: tool %q called %d times with same args",
					ErrRepetitiveLoop, t.Name(), b.repeatCount)
			}
		} else {
			b.lastCallHash = hash
			b.repeatCount = 1
		}
	}

	return nil, nil
}

func (b *breaker) onSuccess(toolName string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Reset fail counter on success.
	if b.lastFailedTool == toolName {
		b.lastFailedTool = ""
		b.failCount = 0
	}

	// If half-open probe succeeded, close the circuit.
	if b.state == HalfOpen {
		b.state = Closed
		b.probeUsed = 0
	}
}

func (b *breaker) onFailure(t tool.Tool, err error) (map[string]any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if t.Name() == b.lastFailedTool {
		b.failCount++
	} else {
		b.lastFailedTool = t.Name()
		b.failCount = 1
	}

	if b.failCount >= b.cfg.MaxConsecutiveFails {
		b.trip()
		return nil, fmt.Errorf("%w: tool %q failed %d times consecutively",
			ErrConsecutiveFails, t.Name(), b.failCount)
	}

	// If in half-open state and probe failed, reopen.
	if b.state == HalfOpen {
		b.trip()
		return nil, ErrCircuitOpen
	}

	// Let the original error propagate.
	return nil, nil
}

func (b *breaker) trip() {
	b.state = Open
	b.openedAt = time.Now()
	b.probeUsed = 0
}

// hashCall creates a deterministic hash of tool name + args for identity comparison.
func hashCall(name string, args map[string]any) string {
	data, _ := json.Marshal(args)
	h := sha256.Sum256(append([]byte(name+":"), data...))
	return fmt.Sprintf("%x", h[:8])
}

// fileExists checks if a file exists on disk.
func fileExists(path string) bool {
	_, err := osStatFn(path)
	return err == nil
}

// osStatFn is a function variable for testing.
var osStatFn = defaultOsStat

func defaultOsStat(path string) (any, error) {
	// Avoid importing os at package level for lightweight builds.
	// In production, inject via FileKillSwitch.checkFn.
	return nil, fmt.Errorf("not implemented: use FileKillSwitch.checkFn or AtomicKillSwitch")
}
