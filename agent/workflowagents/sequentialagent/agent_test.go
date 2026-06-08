package sequentialagent_test

import (
	"fmt"
	"testing"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/workflowagents/sequentialagent"
	"github.com/formonkey/moa/internal/testutil"
)

func TestSequentialExecution(t *testing.T) {
	a1 := testutil.MockAgent("step1", "first")
	a2 := testutil.MockAgent("step2", "second")
	a3 := testutil.MockAgent("step3", "third")

	seq, err := sequentialagent.New(sequentialagent.Config{
		Name:      "pipeline",
		SubAgents: []agent.Agent{a1, a2, a3},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(seq, sess)

	var authors []string
	for evt, err := range seq.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if evt != nil {
			authors = append(authors, evt.Author)
		}
	}
	if len(authors) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(authors), authors)
	}
}

func TestSequentialEmpty(t *testing.T) {
	_, err := sequentialagent.New(sequentialagent.Config{Name: "empty"})
	if err == nil {
		t.Fatal("expected error for empty sub-agents")
	}
}

func TestSequentialWithError(t *testing.T) {
	a1 := testutil.MockAgent("step1", "first")
	a2 := testutil.MockErrorAgent("step2", fmt.Errorf("step2 failed"))

	seq, err := sequentialagent.New(sequentialagent.Config{
		Name:      "pipeline",
		SubAgents: []agent.Agent{a1, a2},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(seq, sess)

	var gotError bool
	for _, err := range seq.Run(ctx) {
		if err != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Fatal("expected error from step2")
	}
}

func TestSequentialContextCancellation(t *testing.T) {
	a1 := testutil.MockAgent("step1", "first")
	a2 := testutil.MockAgent("step2", "second")

	seq, err := sequentialagent.New(sequentialagent.Config{
		Name:      "pipeline",
		SubAgents: []agent.Agent{a1, a2},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(seq, sess)

	// Only consume one event, then stop
	count := 0
	for _, err := range seq.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		count++
		if count >= 1 {
			break
		}
	}
}
