// Package trace provides swarm-level observability by recording agent execution
// events into a structured trace that can be queried, exported to JSON, or
// streamed to a dashboard.
//
// Unlike OpenTelemetry spans (which are for distributed tracing), this trace
// captures swarm-specific semantics: trigger chains, blackboard writes,
// agent durations, circuit breaker hits, and audit results.
package trace

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// EventType classifies trace entries.
type EventType string

const (
	AgentStarted     EventType = "AGENT_STARTED"
	AgentCompleted   EventType = "AGENT_COMPLETED"
	AgentFailed      EventType = "AGENT_FAILED"
	TriggerDispatched EventType = "TRIGGER_DISPATCHED"
	TriggerBlocked   EventType = "TRIGGER_BLOCKED" // circuit breaker
	BlackboardWrite  EventType = "BLACKBOARD_WRITE"
	ReviewPosted     EventType = "REVIEW_POSTED"
	ReviewClaimed    EventType = "REVIEW_CLAIMED"
	AuditResult      EventType = "AUDIT_RESULT"
	RetryAttempt     EventType = "RETRY_ATTEMPT"
	TimeoutHit       EventType = "TIMEOUT_HIT"
)

// Entry is a single trace record.
type Entry struct {
	Timestamp  time.Time         `json:"timestamp"`
	Type       EventType         `json:"type"`
	Agent      string            `json:"agent"`
	Trigger    string            `json:"trigger,omitempty"`
	ChainDepth int               `json:"chain_depth,omitempty"`
	Duration   time.Duration     `json:"duration,omitempty"`
	Error      string            `json:"error,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Recorder collects trace entries thread-safely.
type Recorder struct {
	mu      sync.RWMutex
	entries []Entry
	// OnEntry is called for each new entry (optional, for real-time streaming).
	OnEntry func(Entry)
}

// New creates a new trace recorder.
func New() *Recorder {
	return &Recorder{}
}

// Record adds an entry to the trace.
func (r *Recorder) Record(e Entry) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	r.mu.Lock()
	r.entries = append(r.entries, e)
	r.mu.Unlock()

	if r.OnEntry != nil {
		r.OnEntry(e)
	}
}

// Entries returns a copy of all trace entries.
func (r *Recorder) Entries() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

// EntriesByAgent returns entries for a specific agent.
func (r *Recorder) EntriesByAgent(agentName string) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Entry
	for _, e := range r.entries {
		if e.Agent == agentName {
			out = append(out, e)
		}
	}
	return out
}

// EntriesByType returns entries of a specific type.
func (r *Recorder) EntriesByType(t EventType) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Entry
	for _, e := range r.entries {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// Stats returns aggregate statistics about agent executions.
func (r *Recorder) Stats() map[string]AgentStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make(map[string]AgentStats)
	for _, e := range r.entries {
		s := stats[e.Agent]
		switch e.Type {
		case AgentStarted:
			s.Runs++
		case AgentCompleted:
			s.Successes++
			if e.Duration > 0 {
				s.TotalDuration += e.Duration
				if e.Duration > s.MaxDuration {
					s.MaxDuration = e.Duration
				}
			}
		case AgentFailed:
			s.Failures++
			s.LastError = e.Error
		case RetryAttempt:
			s.Retries++
		case TimeoutHit:
			s.Timeouts++
		}
		stats[e.Agent] = s
	}

	// Compute averages
	for name, s := range stats {
		if s.Successes > 0 {
			s.AvgDuration = s.TotalDuration / time.Duration(s.Successes)
		}
		stats[name] = s
	}

	return stats
}

// AgentStats holds aggregate stats for one agent.
type AgentStats struct {
	Runs          int           `json:"runs"`
	Successes     int           `json:"successes"`
	Failures      int           `json:"failures"`
	Retries       int           `json:"retries"`
	Timeouts      int           `json:"timeouts"`
	TotalDuration time.Duration `json:"total_duration"`
	AvgDuration   time.Duration `json:"avg_duration"`
	MaxDuration   time.Duration `json:"max_duration"`
	LastError     string        `json:"last_error,omitempty"`
}

// JSON exports the entire trace as JSON.
func (r *Recorder) JSON() ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return json.MarshalIndent(r.entries, "", "  ")
}

// Summary prints a human-readable summary of agent execution stats.
func (r *Recorder) Summary() string {
	stats := r.Stats()
	if len(stats) == 0 {
		return "No trace entries recorded."
	}

	var b strings.Builder
	b.WriteString("\n📊 Swarm Execution Summary\n")
	b.WriteString(strings.Repeat("─", 70) + "\n")
	b.WriteString(fmt.Sprintf("%-20s %5s %5s %5s %5s %12s %12s\n",
		"Agent", "Runs", "OK", "Fail", "Retry", "Avg", "Max"))
	b.WriteString(strings.Repeat("─", 70) + "\n")

	for name, s := range stats {
		b.WriteString(fmt.Sprintf("%-20s %5d %5d %5d %5d %12s %12s\n",
			truncate(name, 20),
			s.Runs, s.Successes, s.Failures, s.Retries,
			s.AvgDuration.Round(time.Millisecond),
			s.MaxDuration.Round(time.Millisecond)))
	}
	b.WriteString(strings.Repeat("─", 70) + "\n")
	return b.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// --- Convenience: Scheduler hooks ---

// SchedulerHooks returns OnDispatch/OnComplete functions that record to this trace.
// Use with scheduler.Config{OnDispatch: hooks.OnDispatch, OnComplete: hooks.OnComplete}.
func (r *Recorder) SchedulerHooks() (onDispatch func(string, string, int), onComplete func(string, string, time.Duration, error)) {
	onDispatch = func(agentName, triggerType string, chainDepth int) {
		r.Record(Entry{
			Type:       TriggerDispatched,
			Agent:      agentName,
			Trigger:    triggerType,
			ChainDepth: chainDepth,
		})
		r.Record(Entry{
			Type:  AgentStarted,
			Agent: agentName,
			Metadata: map[string]string{
				"trigger": triggerType,
			},
		})
	}

	onComplete = func(agentName, triggerType string, duration time.Duration, err error) {
		if err != nil {
			r.Record(Entry{
				Type:     AgentFailed,
				Agent:    agentName,
				Trigger:  triggerType,
				Duration: duration,
				Error:    err.Error(),
			})
		} else {
			r.Record(Entry{
				Type:     AgentCompleted,
				Agent:    agentName,
				Trigger:  triggerType,
				Duration: duration,
			})
		}
	}

	return
}
