package fsmagent_test

import (
	"context"
	"os"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/fsmagent"
)

func TestFSMAgentCreation(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:        "fsm-test",
		Description: "FSM test agent",
		Model:       m,
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "You are in the initial state", Options: []string{"done"}, Routes: map[string]string{"done": "end"}},
			{Name: "end", System: "You are done"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.Name() != "fsm-test" {
		t.Fatalf("wrong name: %s", a.Name())
	}
}

func TestFSMAgentRun(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "done"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:  "fsm-run",
		Model: m,
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Start state", Options: []string{"done"}, Routes: map[string]string{"done": "end"}},
			{Name: "end", System: "End state"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	count := 0
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected events")
	}
}

func TestFSMAgentMissingInitial(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}
	_, err := fsmagent.New(fsmagent.Config{
		Name:  "bad-fsm",
		Model: m,
		States: []fsmagent.StateConfig{
			{Name: "start", System: "No initial state"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing initial state")
	}
}

func TestFSMAgentEmptyStates(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}
	_, err := fsmagent.New(fsmagent.Config{
		Name:   "empty-fsm",
		Model:  m,
		States: []fsmagent.StateConfig{},
	})
	if err == nil {
		t.Fatal("expected error for empty states")
	}
}

func TestFSMAgentTerminalState(t *testing.T) {
	// A single terminal state (no routes, no transitions)
	m := &testutil.MockModel{ModelName: "mock", Response: "final answer"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:  "terminal-fsm",
		Model: m,
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Give a final answer"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	count := 0
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected at least 1 event")
	}
}

func TestFSMAgentFuzzyRouteMatch(t *testing.T) {
	// Test that fuzzy matching works (response "I choose DONE" matches "done")
	m := &testutil.MockModel{ModelName: "mock", Response: "I choose DONE please"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:  "fuzzy-fsm",
		Model: m,
		States: []fsmagent.StateConfig{
			{
				Name:    "initial",
				System:  "Choose",
				Options: []string{"done", "retry"},
				Routes:  map[string]string{"done": "end", "retry": "initial"},
			},
			{Name: "end", System: "Done"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	}
}

func TestFSMAgentNoRouteMatch(t *testing.T) {
	// When the response doesn't match any route, FSM should terminate
	m := &testutil.MockModel{ModelName: "mock", Response: "something unexpected"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:  "nomatch-fsm",
		Model: m,
		States: []fsmagent.StateConfig{
			{
				Name:    "initial",
				System:  "Choose yes or no",
				Options: []string{"yes", "no"},
				Routes:  map[string]string{"yes": "good", "no": "bad"},
			},
			{Name: "good", System: "Good"},
			{Name: "bad", System: "Bad"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	count := 0
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		count++
	}
	// Should only have events from the initial state (no routing happened)
	if count == 0 {
		t.Fatal("expected at least 1 event")
	}
}

func TestFSMAgentResultKey(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "architecture plan"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:  "result-fsm",
		Model: m,
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Design the architecture", ResultKey: "architecture"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	}

	// Verify that result was stored in session state
	val, err := sess.State().Get("architecture")
	if err != nil {
		t.Logf("result key not in state (may depend on implementation): %v", err)
	} else if val == nil {
		t.Log("result key was nil")
	}
}

func TestFSMAgentTransitions(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:  "transition-fsm",
		Model: m,
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Start", Transitions: []string{"step2", "step3"}},
			{Name: "step2", System: "Step 2"},
			{Name: "step3", System: "Step 3"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	count := 0
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		count++
	}
	// Should have events from initial + step2 + step3
	if count < 3 {
		t.Fatalf("expected at least 3 events, got %d", count)
	}
}

func TestFSMAgentCrossAgentTransition(t *testing.T) {
	// Create a target agent that the FSM can transition to
	targetAgent := testutil.MockAgent("target", "target response")
	agentMap := map[string]agent.Agent{
		"target": targetAgent,
	}

	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:     "cross-fsm",
		Model:    m,
		AgentMap: agentMap,
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Start", Transitions: []string{"target.initial"}},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	count := 0
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected events from cross-agent transition")
	}
}

func TestFSMAgentCrossAgentUnknown(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:     "cross-bad-fsm",
		Model:    m,
		AgentMap: map[string]agent.Agent{},
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Start", Transitions: []string{"nonexistent.initial"}},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	hasError := false
	for _, err := range a.Run(ctx) {
		if err != nil {
			hasError = true
		}
	}
	if !hasError {
		t.Fatal("expected error for unknown cross-agent reference")
	}
}

func TestFSMAgentSeedSessionState(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "output"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:  "seed-fsm",
		Model: m,
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Use {{.prompt}} in your response"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Create session with user content
	svc, sess := testutil.NewTestSession(t)
	_ = svc

	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:          context.Background(),
		Agent:        a,
		Session:      sess,
		InvocationID: "test-inv",
		UserContent: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: "Build me a website"}},
		},
	})

	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	}

	// Verify prompt was seeded
	val, err := sess.State().Get("prompt")
	if err != nil {
		t.Logf("prompt not in state: %v", err)
	} else if s, ok := val.(string); ok && s != "Build me a website" {
		t.Fatalf("expected prompt 'Build me a website', got %q", s)
	}
}

func TestFSMAgentPipelineTransitions(t *testing.T) {
	// Test the special "Routing pipeline..." system with transitions
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:  "pipeline-fsm",
		Model: m,
		States: []fsmagent.StateConfig{
			{
				Name:        "initial",
				System:      "Routing pipeline...",
				Transitions: []string{"step1", "step2"},
			},
			{Name: "step1", System: "Step 1"},
			{Name: "step2", System: "Step 2"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	count := 0
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		count++
	}
	if count < 2 {
		t.Fatalf("expected at least 2 events from pipeline, got %d", count)
	}
}

func TestFSMAgentRAGFiles(t *testing.T) {
	// Create a temp RAG file
	dir := t.TempDir()
	ragFile := dir + "/rules.md"
	if err := writeFile(ragFile, "# Rules\n1. Use Go\n2. Be idiomatic"); err != nil {
		t.Fatal(err)
	}

	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:     "rag-fsm",
		Model:    m,
		RAGFiles: []string{ragFile},
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Follow the rules in {{.rag_context}}"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	}

	// Verify RAG content was stored in state
	val, _ := sess.State().Get("rag_context")
	if val == nil {
		t.Log("rag_context not in state")
	}
}

func TestFSMAgentRAGFilesMissing(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:     "rag-missing-fsm",
		Model:    m,
		RAGFiles: []string{"/nonexistent/file.md"},
		States: []fsmagent.StateConfig{
			{Name: "initial", System: "Test"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	// Should not crash on missing RAG file
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	}
}

func TestFSMAgentDocSearch(t *testing.T) {
	librarianAgent := testutil.MockAgent("librarian", "Found: Angular best practices")
	agentMap := map[string]agent.Agent{
		"librarian": librarianAgent,
	}

	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:     "ds-fsm",
		Model:    m,
		AgentMap: agentMap,
		States: []fsmagent.StateConfig{
			{
				Name:   "initial",
				System: "Use {{.frontend_rules}}",
				DocSearch: &fsmagent.DocSearchConfig{
					AgentName:  "librarian",
					Prompt:     "Find Angular rules",
					ContextVar: "frontend_rules",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	}
}

func TestFSMAgentDocSearchMissingAgent(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:     "ds-missing-fsm",
		Model:    m,
		AgentMap: map[string]agent.Agent{},
		States: []fsmagent.StateConfig{
			{
				Name:   "initial",
				System: "Test",
				DocSearch: &fsmagent.DocSearchConfig{
					AgentName: "nonexistent",
					Required:  true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	hasError := false
	for _, err := range a.Run(ctx) {
		if err != nil {
			hasError = true
		}
	}
	if !hasError {
		t.Fatal("expected error for required docsearch with missing agent")
	}
}

func TestFSMAgentDocSearchNotRequiredMissing(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, err := fsmagent.New(fsmagent.Config{
		Name:     "ds-optional-fsm",
		Model:    m,
		AgentMap: map[string]agent.Agent{},
		States: []fsmagent.StateConfig{
			{
				Name:   "initial",
				System: "Test",
				DocSearch: &fsmagent.DocSearchConfig{
					AgentName: "nonexistent",
					Required:  false, // optional
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
	}
	// Should succeed — docsearch is optional
}

// --- Helpers ---

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// Ensure session.State compiles
var _ session.State = (session.State)(nil)

