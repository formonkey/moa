// Package costgovernor provides a multi-layered cost enforcement plugin for moa agents.
//
// Unlike costtracker (which only records usage), costgovernor actively enforces
// budget limits at multiple granularities: per-task token budget, dollar budget,
// per-tool call caps, and financial velocity limits.
//
// Designed for local model deployments where runaway loops can burn through
// cloud API credits or waste compute time.
//
// Usage:
//
//	gov := costgovernor.New(costgovernor.Config{
//	    MaxTotalTokens:      100_000,
//	    MaxDollarBudget:     0.50,
//	    PricePerInputToken:  0.00000015,
//	    PricePerOutputToken: 0.0000006,
//	    ToolCallCaps:        map[string]int{"run_command": 10},
//	})
//	runner, _ := runner.New(runner.Config{
//	    Agent:   myAgent,
//	    Plugins: []*plugin.Plugin{gov},
//	})
package costgovernor

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/tool"
)

// Errors returned when budget limits are breached.
var (
	ErrBudgetExhausted  = errors.New("costgovernor: token budget exhausted")
	ErrDollarBudget     = errors.New("costgovernor: dollar budget exhausted")
	ErrToolCapExceeded  = errors.New("costgovernor: tool call cap exceeded")
	ErrVelocityLimit    = errors.New("costgovernor: financial velocity limit exceeded")
	ErrWallTimeExceeded = errors.New("costgovernor: wall time exceeded")
)

// BehaviorOnBreach defines what happens when a budget limit is hit.
type BehaviorOnBreach int

const (
	// Abort stops the agent immediately with an error.
	Abort BehaviorOnBreach = iota
	// Warn logs a warning but allows the agent to continue.
	Warn
)

// Config configures the cost governor plugin.
type Config struct {
	// Per-task token limit (input+output combined). 0 = unlimited.
	MaxTotalTokens int
	// Per-task dollar budget. 0 = unlimited.
	MaxDollarBudget float64
	// Per-tool call caps. Tool name -> max calls allowed.
	ToolCallCaps map[string]int

	// Financial velocity: max dollars spent per minute. 0 = disabled.
	MaxDollarsPerMinute float64

	// Pricing (required for dollar-based limits).
	PricePerInputToken  float64
	PricePerOutputToken float64

	// MaxWallTime limits total elapsed time from first model call.
	// Ideal for local models where the cost is GPU/CPU time, not dollars.
	// 0 = unlimited.
	MaxWallTime time.Duration

	// Behavior when a limit is hit. Default: Abort.
	OnBreach BehaviorOnBreach
}

// state holds the runtime state of the governor.
type state struct {
	mu sync.Mutex

	// Token counters
	totalInputTokens  int
	totalOutputTokens int

	// Tool call counters
	toolCalls map[string]int

	// Velocity tracking
	spendLog []spendEntry

	// Wall time tracking
	startedAt time.Time
}

// spendEntry records a spend event for velocity tracking.
type spendEntry struct {
	at     time.Time
	amount float64
}

// New creates a cost governor plugin.
func New(cfg Config) *plugin.Plugin {
	if cfg.ToolCallCaps == nil {
		cfg.ToolCallCaps = make(map[string]int)
	}

	s := &state{
		toolCalls: make(map[string]int),
	}

	return &plugin.Plugin{
		Name: "cost-governor",

		BeforeModelCallback: func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			return beforeModel(ctx, req, &cfg, s)
		},

		AfterModelCallback: func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
			return afterModel(ctx, resp, &cfg, s)
		},

		BeforeToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			return beforeTool(ctx, t, args, &cfg, s)
		},
	}
}

// beforeModel checks budget before each LLM call.
func beforeModel(_ agent.CallbackContext, _ *model.LLMRequest, cfg *Config, s *state) (*model.LLMResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check token budget
	if cfg.MaxTotalTokens > 0 {
		total := s.totalInputTokens + s.totalOutputTokens
		if total >= cfg.MaxTotalTokens {
			return nil, ErrBudgetExhausted
		}
	}

	// Check dollar budget
	if cfg.MaxDollarBudget > 0 {
		spent := s.currentSpend(cfg)
		if spent >= cfg.MaxDollarBudget {
			return nil, ErrDollarBudget
		}
	}

	// Check wall time
	if cfg.MaxWallTime > 0 {
		if s.startedAt.IsZero() {
			s.startedAt = time.Now()
		} else if time.Since(s.startedAt) > cfg.MaxWallTime {
			return nil, ErrWallTimeExceeded
		}
	}

	return nil, nil
}

// afterModel records token usage and checks velocity.
func afterModel(_ agent.CallbackContext, resp *model.LLMResponse, cfg *Config, s *state) (*model.LLMResponse, error) {
	if resp == nil || resp.UsageMetadata == nil {
		return nil, nil
	}

	inputTokens := int(resp.UsageMetadata.PromptTokenCount)
	outputTokens := int(resp.UsageMetadata.CandidatesTokenCount)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalInputTokens += inputTokens
	s.totalOutputTokens += outputTokens

	// Calculate cost of this call.
	callCost := float64(inputTokens)*cfg.PricePerInputToken +
		float64(outputTokens)*cfg.PricePerOutputToken

	// Record for velocity tracking.
	if cfg.MaxDollarsPerMinute > 0 {
		s.spendLog = append(s.spendLog, spendEntry{
			at:     time.Now(),
			amount: callCost,
		})

		// Check velocity
		velocity := s.velocityLastMinute()
		if velocity > cfg.MaxDollarsPerMinute {
			return nil, ErrVelocityLimit
		}
	}

	return nil, nil
}

// beforeTool checks per-tool call caps.
func beforeTool(_ agent.CallbackContext, t tool.Tool, _ map[string]any, cfg *Config, s *state) (map[string]any, error) {
	if len(cfg.ToolCallCaps) == 0 {
		return nil, nil
	}

	cap, hasCap := cfg.ToolCallCaps[t.Name()]
	if !hasCap {
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.toolCalls[t.Name()]++
	if s.toolCalls[t.Name()] > cap {
		return nil, fmt.Errorf("%w: tool %q exceeded cap of %d calls", ErrToolCapExceeded, t.Name(), cap)
	}

	return nil, nil
}

// currentSpend calculates the current total spend. Must be called with s.mu held.
func (s *state) currentSpend(cfg *Config) float64 {
	return float64(s.totalInputTokens)*cfg.PricePerInputToken +
		float64(s.totalOutputTokens)*cfg.PricePerOutputToken
}

// velocityLastMinute calculates dollars spent in the last 60 seconds. Must be called with s.mu held.
func (s *state) velocityLastMinute() float64 {
	cutoff := time.Now().Add(-time.Minute)
	var total float64
	validStart := 0
	for i, e := range s.spendLog {
		if e.at.Before(cutoff) {
			validStart = i + 1
			continue
		}
		total += e.amount
	}
	// Prune old entries.
	if validStart > 0 {
		s.spendLog = s.spendLog[validStart:]
	}
	return total
}
