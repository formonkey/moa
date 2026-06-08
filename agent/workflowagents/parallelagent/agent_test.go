package parallelagent_test

import (
	"fmt"
	"testing"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/workflowagents/parallelagent"
	"github.com/formonkey/moa/internal/testutil"
)

func TestParallelExecution(t *testing.T) {
	a1 := testutil.MockAgent("worker1", "result1")
	a2 := testutil.MockAgent("worker2", "result2")

	par, err := parallelagent.New(parallelagent.Config{
		Name:      "parallel",
		SubAgents: []agent.Agent{a1, a2},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(par, sess)

	count := 0
	for _, err := range par.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 events, got %d", count)
	}
}

func TestParallelEmpty(t *testing.T) {
	_, err := parallelagent.New(parallelagent.Config{Name: "empty"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParallelWithError(t *testing.T) {
	a1 := testutil.MockAgent("worker1", "result1")
	a2 := testutil.MockErrorAgent("worker2", fmt.Errorf("worker2 failed"))

	par, err := parallelagent.New(parallelagent.Config{
		Name:      "parallel",
		SubAgents: []agent.Agent{a1, a2},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(par, sess)

	var gotError bool
	for _, err := range par.Run(ctx) {
		if err != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Fatal("expected error from worker2")
	}
}

func TestParallelEarlyExit(t *testing.T) {
	a1 := testutil.MockAgent("worker1", "result1")
	a2 := testutil.MockAgent("worker2", "result2")
	a3 := testutil.MockAgent("worker3", "result3")

	par, err := parallelagent.New(parallelagent.Config{
		Name:      "parallel",
		SubAgents: []agent.Agent{a1, a2, a3},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(par, sess)

	// Only consume one event, then stop
	count := 0
	for _, err := range par.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		count++
		if count >= 1 {
			break
		}
	}
}
