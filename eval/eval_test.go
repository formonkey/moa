package eval_test

import (
	"context"
	"testing"

	"github.com/formonkey/moa/eval"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/session"
)

func TestSuiteBasic(t *testing.T) {
	suite := eval.NewSuite("Basic Tests")
	suite.Add(eval.TestCase{
		Name:   "greeting",
		Input:  "hello",
		Assert: eval.Contains("mock"),
	})

	a := testutil.MockAgent("test", "mock response")
	results := suite.Run(context.Background(), a, session.InMemoryService())
	if results.Passed != 1 {
		t.Fatalf("expected 1 passed, got %d: %s", results.Passed, results.Summary())
	}
}

func TestSuiteFailure(t *testing.T) {
	suite := eval.NewSuite("Fail Tests")
	suite.Add(eval.TestCase{
		Name:   "will-fail",
		Input:  "hello",
		Assert: eval.Contains("something else entirely"),
	})

	a := testutil.MockAgent("test", "mock response")
	results := suite.Run(context.Background(), a, session.InMemoryService())
	if results.Failed != 1 {
		t.Fatalf("expected 1 failure, got %d", results.Failed)
	}
	summary := results.Summary()
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestContains(t *testing.T) {
	f := eval.Contains("world")
	if err := f("hello world"); err != nil {
		t.Fatal(err)
	}
	if err := f("hello"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMatchesRegex(t *testing.T) {
	f := eval.MatchesRegex(`\d{3}-\d{4}`)
	if err := f("call 555-1234"); err != nil {
		t.Fatal(err)
	}
	if err := f("no numbers"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAll(t *testing.T) {
	f := eval.All(
		eval.Contains("hello"),
		eval.Contains("world"),
	)
	if err := f("hello world"); err != nil {
		t.Fatal(err)
	}
	if err := f("hello"); err == nil {
		t.Fatal("expected error — missing 'world'")
	}
}

func TestSuiteWithInitialState(t *testing.T) {
	suite := eval.NewSuite("State Tests")
	suite.Add(eval.TestCase{
		Name:         "with-state",
		Input:        "test",
		Assert:       eval.Contains("mock"),
		InitialState: map[string]any{"key": "val"},
	})

	a := testutil.MockAgent("test", "mock response")
	results := suite.Run(context.Background(), a, session.InMemoryService())
	if results.Failed != 0 {
		t.Fatalf("unexpected failure: %s", results.Summary())
	}
}
