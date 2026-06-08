package competitiveagent_test

import (
	"fmt"
	"testing"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/workflowagents/competitiveagent"
	"github.com/formonkey/moa/internal/testutil"
)

func TestCompetitiveExecution(t *testing.T) {
	a1 := testutil.MockAgent("fast", "I'm fast")
	a2 := testutil.MockAgent("slow", "I'm slow")

	comp, err := competitiveagent.New(competitiveagent.Config{
		Name:    "race",
		Workers: []agent.Agent{a1, a2},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(comp, sess)

	count := 0
	for _, err := range comp.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected events")
	}
}

func TestCompetitiveTooFewWorkers(t *testing.T) {
	a1 := testutil.MockAgent("only", "solo")
	_, err := competitiveagent.New(competitiveagent.Config{
		Name:    "race",
		Workers: []agent.Agent{a1},
	})
	if err == nil {
		t.Fatal("expected error for < 2 workers")
	}
}

func TestCompetitiveWithExplicitVoters(t *testing.T) {
	a1 := testutil.MockAgent("w1", "solution1")
	a2 := testutil.MockAgent("w2", "solution2")
	// Voter that votes for "w1"
	voter := testutil.MockAgent("judge", "w1")

	comp, err := competitiveagent.New(competitiveagent.Config{
		Name:    "race",
		Workers: []agent.Agent{a1, a2},
		Voters:  []agent.Agent{voter},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(comp, sess)

	for _, err := range comp.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}

func TestCompetitiveWithFailingWorker(t *testing.T) {
	a1 := testutil.MockAgent("good", "I'm good")
	a2 := testutil.MockErrorAgent("bad", fmt.Errorf("worker failed"))

	comp, err := competitiveagent.New(competitiveagent.Config{
		Name:    "race",
		Workers: []agent.Agent{a1, a2},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(comp, sess)

	for _, err := range comp.Run(ctx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestCompetitiveAllWorkersFail(t *testing.T) {
	a1 := testutil.MockErrorAgent("bad1", fmt.Errorf("fail1"))
	a2 := testutil.MockErrorAgent("bad2", fmt.Errorf("fail2"))

	comp, err := competitiveagent.New(competitiveagent.Config{
		Name:    "race",
		Workers: []agent.Agent{a1, a2},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(comp, sess)

	var gotError bool
	for _, err := range comp.Run(ctx) {
		if err != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Fatal("expected error when all workers fail")
	}
}

func TestCompetitiveWithFailingVoter(t *testing.T) {
	a1 := testutil.MockAgent("w1", "solution1")
	a2 := testutil.MockAgent("w2", "solution2")
	voter := testutil.MockErrorAgent("judge", fmt.Errorf("voter failed"))

	comp, err := competitiveagent.New(competitiveagent.Config{
		Name:    "race",
		Workers: []agent.Agent{a1, a2},
		Voters:  []agent.Agent{voter},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(comp, sess)

	// Should still produce a result even with failed voter (abstention)
	for _, err := range comp.Run(ctx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestCompetitiveWithDuplicateVoterWorker(t *testing.T) {
	a1 := testutil.MockAgent("w1", "solution1")
	a2 := testutil.MockAgent("w2", "solution2")

	// Use a1 as both worker and voter — buildSubAgents should dedup
	comp, err := competitiveagent.New(competitiveagent.Config{
		Name:    "race",
		Workers: []agent.Agent{a1, a2},
		Voters:  []agent.Agent{a1},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(comp, sess)

	for _, err := range comp.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}
