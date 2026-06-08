package trace_test

import (
	"testing"
	"time"

	"github.com/formonkey/moa/swarm/trace"
)

func TestRecordAndEntries(t *testing.T) {
	r := trace.New()

	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "coder"})
	r.Record(trace.Entry{Type: trace.AgentCompleted, Agent: "coder", Duration: 2 * time.Second})
	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "reviewer"})
	r.Record(trace.Entry{Type: trace.AgentFailed, Agent: "reviewer", Error: "timeout"})

	entries := r.Entries()
	if len(entries) != 4 {
		t.Fatalf("expected 4, got %d", len(entries))
	}
	// Verify timestamps auto-set
	for _, e := range entries {
		if e.Timestamp.IsZero() {
			t.Fatal("expected non-zero timestamp")
		}
	}
}

func TestEntriesByAgent(t *testing.T) {
	r := trace.New()
	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "A"})
	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "B"})
	r.Record(trace.Entry{Type: trace.AgentCompleted, Agent: "A", Duration: time.Second})

	aEntries := r.EntriesByAgent("A")
	if len(aEntries) != 2 {
		t.Fatalf("expected 2, got %d", len(aEntries))
	}
}

func TestEntriesByType(t *testing.T) {
	r := trace.New()
	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "A"})
	r.Record(trace.Entry{Type: trace.AgentFailed, Agent: "B", Error: "err"})
	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "C"})

	started := r.EntriesByType(trace.AgentStarted)
	if len(started) != 2 {
		t.Fatalf("expected 2, got %d", len(started))
	}
}

func TestStats(t *testing.T) {
	r := trace.New()
	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "coder"})
	r.Record(trace.Entry{Type: trace.AgentCompleted, Agent: "coder", Duration: 2 * time.Second})
	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "coder"})
	r.Record(trace.Entry{Type: trace.AgentCompleted, Agent: "coder", Duration: 4 * time.Second})
	r.Record(trace.Entry{Type: trace.RetryAttempt, Agent: "coder"})

	stats := r.Stats()
	s, ok := stats["coder"]
	if !ok {
		t.Fatal("expected stats for 'coder'")
	}
	if s.Runs != 2 {
		t.Fatalf("expected 2 runs, got %d", s.Runs)
	}
	if s.Successes != 2 {
		t.Fatalf("expected 2 successes, got %d", s.Successes)
	}
	if s.Retries != 1 {
		t.Fatalf("expected 1 retry, got %d", s.Retries)
	}
	if s.AvgDuration != 3*time.Second {
		t.Fatalf("expected 3s avg, got %s", s.AvgDuration)
	}
	if s.MaxDuration != 4*time.Second {
		t.Fatalf("expected 4s max, got %s", s.MaxDuration)
	}
}

func TestJSON(t *testing.T) {
	r := trace.New()
	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "test"})

	data, err := r.JSON()
	if err != nil {
		t.Fatalf("JSON failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSON")
	}
}

func TestSummary(t *testing.T) {
	r := trace.New()

	// Empty
	s := r.Summary()
	if s == "" {
		t.Fatal("expected non-empty summary even with no entries")
	}

	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "agent1"})
	r.Record(trace.Entry{Type: trace.AgentCompleted, Agent: "agent1", Duration: time.Second})

	s = r.Summary()
	if s == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestSchedulerHooks(t *testing.T) {
	r := trace.New()
	onDispatch, onComplete := r.SchedulerHooks()

	onDispatch("agent1", "FILE_MODIFIED", 0)
	onComplete("agent1", "FILE_MODIFIED", 500*time.Millisecond, nil)

	entries := r.Entries()
	// onDispatch records 2 entries: TriggerDispatched + AgentStarted
	// onComplete records 1 entry: AgentCompleted
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
}

func TestOnEntryCallback(t *testing.T) {
	r := trace.New()
	var received []trace.Entry
	r.OnEntry = func(e trace.Entry) {
		received = append(received, e)
	}

	r.Record(trace.Entry{Type: trace.AgentStarted, Agent: "test"})
	if len(received) != 1 {
		t.Fatalf("expected 1 callback, got %d", len(received))
	}
}
