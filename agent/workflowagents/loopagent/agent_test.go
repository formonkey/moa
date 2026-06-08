package loopagent_test

import (
	"context"
	"fmt"
	"iter"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/workflowagents/loopagent"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
)

func TestLoopWithMaxIterations(t *testing.T) {
	counter := 0
	inner, _ := agent.New(agent.Config{
		Name: "counter",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				counter++
				evt := session.NewEvent(ctx.InvocationID())
				evt.Author = "counter"
				evt.LLMResponse = model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "iteration"}},
					},
				}
				yield(evt, nil)
			}
		},
	})

	loop, err := loopagent.New(loopagent.Config{
		Name:          "loop",
		SubAgent:      inner,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(loop, sess)

	for _, err := range loop.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
	if counter != 3 {
		t.Fatalf("expected 3 iterations, got %d", counter)
	}
}

func TestLoopRequiresSubAgent(t *testing.T) {
	_, err := loopagent.New(loopagent.Config{Name: "empty"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoopDefaultMaxIterations(t *testing.T) {
	counter := 0
	inner, _ := agent.New(agent.Config{
		Name: "counter",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				counter++
				evt := session.NewEvent(ctx.InvocationID())
				evt.Author = "counter"
				evt.LLMResponse = model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "iteration"}},
					},
				}
				yield(evt, nil)
			}
		},
	})

	// MaxIterations=0 should default to 10
	loop, err := loopagent.New(loopagent.Config{
		Name:     "loop",
		SubAgent: inner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(loop, sess)

	for _, err := range loop.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
	if counter != 10 {
		t.Fatalf("expected 10 iterations (default), got %d", counter)
	}
}

func TestLoopWithError(t *testing.T) {
	inner := testutil.MockErrorAgent("bad", fmt.Errorf("loop error"))

	loop, err := loopagent.New(loopagent.Config{
		Name:          "loop",
		SubAgent:      inner,
		MaxIterations: 5,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(loop, sess)

	var gotError bool
	for _, err := range loop.Run(ctx) {
		if err != nil {
			gotError = true
		}
	}
	if !gotError {
		t.Fatal("expected error from sub-agent")
	}
}

func TestLoopWithEscalate(t *testing.T) {
	callCount := 0
	inner, _ := agent.New(agent.Config{
		Name: "escalator",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				callCount++
				evt := session.NewEvent(ctx.InvocationID())
				evt.Author = "escalator"
				evt.LLMResponse = model.LLMResponse{
					Content: &genai.Content{
						Role: "model", Parts: []*genai.Part{{Text: "done"}},
					},
				}
				// Signal escalation on second call
				if callCount >= 2 {
					evt.Actions.Escalate = true
				}
				yield(evt, nil)
			}
		},
	})

	loop, _ := loopagent.New(loopagent.Config{
		Name:          "loop",
		SubAgent:      inner,
		MaxIterations: 10,
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(loop, sess)

	for _, err := range loop.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
	// Should have stopped at 2 or 3 (exit happens after escalate is detected)
	if callCount > 3 {
		t.Fatalf("expected early exit via escalate, but ran %d times", callCount)
	}
}

func TestLoopEarlyConsumerExit(t *testing.T) {
	inner := testutil.MockAgent("worker", "output")

	loop, _ := loopagent.New(loopagent.Config{
		Name:          "loop",
		SubAgent:      inner,
		MaxIterations: 10,
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(loop, sess)

	count := 0
	for _, err := range loop.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		count++
		if count >= 1 {
			break
		}
	}
}

func TestExitLoopTool(t *testing.T) {
	exitTool := &loopagent.ExitLoopTool{}
	if exitTool.Name() != "exit_loop" {
		t.Fatal("wrong name")
	}
	if exitTool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
	if exitTool.IsNative() {
		t.Fatal("should not be native")
	}
	if exitTool.IsLongRunning() {
		t.Fatal("should not be long running")
	}
	if exitTool.Declaration() == nil {
		t.Fatal("expected declaration")
	}

	// Test Execute
	result, err := exitTool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result["_escalate"] != true {
		t.Fatal("expected _escalate=true in result")
	}
}
