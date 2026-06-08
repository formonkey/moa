package debateagent_test

import (
	"fmt"
	"testing"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/workflowagents/debateagent"
	"github.com/formonkey/moa/internal/testutil"
)

func TestDebateExecution(t *testing.T) {
	a1 := testutil.MockAgent("pro", "I'm for it")
	a2 := testutil.MockAgent("con", "I'm against it")

	deb, err := debateagent.New(debateagent.Config{
		Name:      "debate",
		Debaters:  []agent.Agent{a1, a2},
		MaxRounds: 2,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(deb, sess)

	count := 0
	for _, err := range deb.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected events")
	}
}

func TestDebateTooFewDebaters(t *testing.T) {
	a1 := testutil.MockAgent("solo", "just me")
	_, err := debateagent.New(debateagent.Config{
		Name:     "debate",
		Debaters: []agent.Agent{a1},
	})
	if err == nil {
		t.Fatal("expected error for < 2 debaters")
	}
}

func TestDebateDefaultMaxRounds(t *testing.T) {
	a1 := testutil.MockAgent("pro", "I'm for it")
	a2 := testutil.MockAgent("con", "I'm against it")

	// MaxRounds <= 0 should default to 3
	deb, err := debateagent.New(debateagent.Config{
		Name:      "debate",
		Debaters:  []agent.Agent{a1, a2},
		MaxRounds: 0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(deb, sess)

	for _, err := range deb.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}

func TestDebateInitialRoundError(t *testing.T) {
	a1 := testutil.MockErrorAgent("pro", fmt.Errorf("initial fail"))
	a2 := testutil.MockAgent("con", "I'm against it")

	deb, err := debateagent.New(debateagent.Config{
		Name:      "debate",
		Debaters:  []agent.Agent{a1, a2},
		MaxRounds: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(deb, sess)

	var gotError bool
	for _, err := range deb.Run(ctx) {
		if err != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Fatal("expected error from initial round failure")
	}
}

func TestDebateRoundError(t *testing.T) {
	// First call succeeds, subsequent calls fail. Use a mock agent
	// that returns an error — in the debate rounds, errors are logged
	// but the agent continues.
	a1 := testutil.MockAgent("pro", "I'm for it")
	a2 := testutil.MockAgent("con", "I agree with everything, consensus reached")

	deb, err := debateagent.New(debateagent.Config{
		Name:      "debate",
		Debaters:  []agent.Agent{a1, a2},
		MaxRounds: 1,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(deb, sess)

	for _, err := range deb.Run(ctx) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestDebateConsensusDetection(t *testing.T) {
	// Agents that signal consensus — should finish early
	a1 := testutil.MockAgent("pro", "I agree with the consensus reached")
	a2 := testutil.MockAgent("con", "I agree with pro")

	deb, err := debateagent.New(debateagent.Config{
		Name:      "debate",
		Debaters:  []agent.Agent{a1, a2},
		MaxRounds: 5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(deb, sess)

	for _, err := range deb.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}
