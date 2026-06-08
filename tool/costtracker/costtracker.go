// Package costtracker provides token usage and cost tracking for cloud LLM models.
//
// It tracks input/output tokens per agent, per state, and per subtask,
// providing a cost summary at the end of a task.
//
// Usage:
//
//	tracker := costtracker.New(costtracker.Config{PricePerInputToken: 0.00000015, PricePerOutputToken: 0.0000006})
//	costtracker.Register(tracker)
//	// Tools available: cost_summary
package costtracker

import (
	"context"
	"fmt"
	"sync"

	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/functiontool"
)

// Config for the cost tracker.
type Config struct {
	PricePerInputToken  float64 // e.g., $0.00000015 for Gemini Flash
	PricePerOutputToken float64 // e.g., $0.0000006 for Gemini Flash
	Currency            string  // default "USD"
}

// Entry records token usage for a single operation.
type Entry struct {
	Agent        string `json:"agent"`
	State        string `json:"state"`
	SubtaskID    string `json:"subtask_id,omitempty"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// Tracker accumulates token usage and calculates costs.
type Tracker struct {
	mu      sync.Mutex
	config  Config
	entries []Entry
}

// New creates a cost tracker.
func New(cfg Config) *Tracker {
	if cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	return &Tracker{config: cfg}
}

// Record adds a token usage entry.
func (t *Tracker) Record(agent, state, subtaskID string, inputTokens, outputTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, Entry{
		Agent:        agent,
		State:        state,
		SubtaskID:    subtaskID,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	})
}

// Summary returns aggregated costs.
type Summary struct {
	TotalInputTokens  int                `json:"total_input_tokens"`
	TotalOutputTokens int                `json:"total_output_tokens"`
	TotalTokens       int                `json:"total_tokens"`
	TotalCost         float64            `json:"total_cost"`
	Currency          string             `json:"currency"`
	ByAgent           map[string]AgentCost `json:"by_agent"`
	Entries           int                `json:"entries"`
}

// AgentCost is the cost breakdown for a single agent.
type AgentCost struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// GetSummary calculates and returns the cost summary.
func (t *Tracker) GetSummary() Summary {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := Summary{
		Currency: t.config.Currency,
		ByAgent:  make(map[string]AgentCost),
		Entries:  len(t.entries),
	}

	for _, e := range t.entries {
		s.TotalInputTokens += e.InputTokens
		s.TotalOutputTokens += e.OutputTokens

		ac := s.ByAgent[e.Agent]
		ac.InputTokens += e.InputTokens
		ac.OutputTokens += e.OutputTokens
		ac.Cost += float64(e.InputTokens)*t.config.PricePerInputToken +
			float64(e.OutputTokens)*t.config.PricePerOutputToken
		s.ByAgent[e.Agent] = ac
	}

	s.TotalTokens = s.TotalInputTokens + s.TotalOutputTokens
	s.TotalCost = float64(s.TotalInputTokens)*t.config.PricePerInputToken +
		float64(s.TotalOutputTokens)*t.config.PricePerOutputToken

	return s
}

// Reset clears all entries.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = nil
}

// --- Tools ---

// Register creates cost tracker tools and registers them.
func Register(tracker *Tracker) error {
	tools, err := NewToolset(tracker)
	if err != nil {
		return fmt.Errorf("costtracker: %w", err)
	}
	for _, t := range tools {
		toolRef := t
		_ = configurable.RegisterToolFactory(t.Name(), func(ctx context.Context, args map[string]any) (tool.Tool, error) {
			return toolRef, nil
		})
	}
	return nil
}

// NewToolset creates cost tracker tools.
func NewToolset(tracker *Tracker) ([]tool.Tool, error) {
	summary, err := newCostSummary(tracker)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{summary}, nil
}

// --- cost_summary ---

type costSummaryArgs struct{}
type costSummaryResult struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	TotalTokens  int     `json:"total_tokens"`
	TotalCost    string  `json:"total_cost"`
	Currency     string  `json:"currency"`
	ByAgent      map[string]string `json:"by_agent"`
	Entries      int     `json:"entries"`
}

func newCostSummary(tracker *Tracker) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "cost_summary",
		Description: "Show the token usage and cost summary for the current task. Only relevant for cloud models.",
	}, func(ctx context.Context, args costSummaryArgs) (costSummaryResult, error) {
		s := tracker.GetSummary()

		byAgent := make(map[string]string)
		for name, ac := range s.ByAgent {
			byAgent[name] = fmt.Sprintf("%d in + %d out = $%.6f", ac.InputTokens, ac.OutputTokens, ac.Cost)
		}

		return costSummaryResult{
			InputTokens:  s.TotalInputTokens,
			OutputTokens: s.TotalOutputTokens,
			TotalTokens:  s.TotalTokens,
			TotalCost:    fmt.Sprintf("$%.6f", s.TotalCost),
			Currency:     s.Currency,
			ByAgent:      byAgent,
			Entries:      s.Entries,
		}, nil
	})
}
