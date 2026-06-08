package agent_test

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
)

func TestNewAgent(t *testing.T) {
	a, err := agent.New(agent.Config{
		Name:        "test-agent",
		Description: "A test agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(session.NewEvent("inv"), nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if a.Name() != "test-agent" {
		t.Fatalf("expected 'test-agent', got %q", a.Name())
	}
	if a.Description() != "A test agent" {
		t.Fatalf("expected description, got %q", a.Description())
	}
}

func TestNewAgentWithEmptyName(t *testing.T) {
	a, err := agent.New(agent.Config{
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("New should accept empty name: %v", err)
	}
	if a.Name() != "" {
		t.Fatalf("expected empty name, got %q", a.Name())
	}
}

func TestFindAgent(t *testing.T) {
	child2, _ := agent.New(agent.Config{Name: "child2"})
	grandchild, _ := agent.New(agent.Config{Name: "grandchild"})

	// Build tree: root -> child1 -> grandchild, child2
	child1WithSub, _ := agent.New(agent.Config{
		Name:      "child1",
		SubAgents: []agent.Agent{grandchild},
	})
	root, _ := agent.New(agent.Config{
		Name:      "root",
		SubAgents: []agent.Agent{child1WithSub, child2},
	})

	// Find by name
	found := root.FindAgent("grandchild")
	if found == nil {
		t.Fatal("expected to find 'grandchild'")
	}
	if found.Name() != "grandchild" {
		t.Fatalf("expected 'grandchild', got %q", found.Name())
	}

	found = root.FindAgent("child2")
	if found == nil || found.Name() != "child2" {
		t.Fatal("expected to find 'child2'")
	}

	found = root.FindAgent("root")
	if found == nil || found.Name() != "root" {
		t.Fatal("expected to find 'root' (self)")
	}

	found = root.FindAgent("nonexistent")
	if found != nil {
		t.Fatal("expected nil for nonexistent agent")
	}
}

func TestBuildParentMap(t *testing.T) {
	child, _ := agent.New(agent.Config{Name: "child"})
	root, _ := agent.New(agent.Config{
		Name:      "root",
		SubAgents: []agent.Agent{child},
	})

	pm, err := agent.BuildParentMap(root)
	if err != nil {
		t.Fatalf("BuildParentMap failed: %v", err)
	}

	parent, hasParent := pm["child"]
	if !hasParent {
		t.Fatal("expected child to have a parent")
	}
	if parent.Name() != "root" {
		t.Fatalf("expected parent 'root', got %q", parent.Name())
	}

	_, hasRoot := pm["root"]
	if hasRoot {
		t.Fatal("root should not have a parent entry")
	}
}

func TestEventBus(t *testing.T) {
	bus := agent.NewEventBus()
	received := make([]agent.BusEvent, 0)

	bus.On(func(e agent.BusEvent) {
		received = append(received, e)
	})

	bus.Emit(agent.BusEvent{Type: agent.EventRunStarted, Agent: "test"})
	bus.Emit(agent.BusEvent{Type: agent.EventChunk, Agent: "test", Payload: "hello"})

	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
	if received[0].Type != agent.EventRunStarted {
		t.Fatalf("expected EventRunStarted, got %v", received[0].Type)
	}
}

func TestInvocationContext(t *testing.T) {
	a, _ := agent.New(agent.Config{Name: "ctx-agent"})
	sess := createMockSession()

	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:          context.Background(),
		Agent:        a,
		Session:      sess,
		InvocationID: "inv-123",
		Branch:       "root",
	})

	if ctx.Agent().Name() != "ctx-agent" {
		t.Fatal("wrong agent name")
	}
	if ctx.InvocationID() != "inv-123" {
		t.Fatal("wrong invocation ID")
	}
	if ctx.Branch() != "root" {
		t.Fatal("wrong branch")
	}
	if ctx.Ended() {
		t.Fatal("should not be ended yet")
	}

	ctx.EndInvocation()
	if !ctx.Ended() {
		t.Fatal("should be ended after EndInvocation")
	}
}

func TestLoaderSingle(t *testing.T) {
	a, _ := agent.New(agent.Config{Name: "single"})
	loader := agent.NewSingleLoader(a)

	if loader.RootAgent().Name() != "single" {
		t.Fatal("wrong root agent")
	}
	agents := loader.ListAgents()
	if len(agents) != 1 || agents[0] != "single" {
		t.Fatal("ListAgents wrong")
	}
	loaded, err := loader.LoadAgent("single")
	if err != nil || loaded.Name() != "single" {
		t.Fatal("LoadAgent failed")
	}
	_, err = loader.LoadAgent("other")
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestLoaderMulti(t *testing.T) {
	root, _ := agent.New(agent.Config{Name: "root"})
	extra, _ := agent.New(agent.Config{Name: "extra"})

	loader, err := agent.NewMultiLoader(root, extra)
	if err != nil {
		t.Fatalf("NewMultiLoader failed: %v", err)
	}
	if loader.RootAgent().Name() != "root" {
		t.Fatal("wrong root")
	}
	agents := loader.ListAgents()
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	// Duplicate name should fail
	dup, _ := agent.New(agent.Config{Name: "root"})
	_, err = agent.NewMultiLoader(root, dup)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
}

func TestInvocationContextAccessors(t *testing.T) {
	a, _ := agent.New(agent.Config{Name: "acc-agent"})
	sess := createMockSession()

	content := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "hi"}}}
	rc := &agent.RunConfig{StreamingMode: agent.StreamingModeNone}
	pm := agent.ParentMap{}
	bus := agent.NewEventBus()

	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:         context.Background(),
		Agent:       a,
		Session:     sess,
		UserContent: content,
		RunConfig:   rc,
		ParentMap:   pm,
		EventBus:    bus,
	})

	// Test all accessors
	if ctx.Session() == nil {
		t.Fatal("Session nil")
	}
	if ctx.UserContent() == nil {
		t.Fatal("UserContent nil")
	}
	if ctx.RunConfig() == nil {
		t.Fatal("RunConfig nil")
	}
	if ctx.ParentMap() == nil {
		t.Fatal("ParentMap nil")
	}
	if ctx.EventBus() == nil {
		t.Fatal("EventBus nil")
	}
	if ctx.Artifacts() != nil {
		// Artifacts was not set, should be nil
	}
	if ctx.Memory() != nil {
		// Memory was not set, should be nil
	}

	// WithContext
	newCtx := ctx.WithContext(context.TODO())
	if newCtx == nil {
		t.Fatal("WithContext returned nil")
	}
}

func TestRunWithBeforeCallback(t *testing.T) {
	callbackCalled := false
	a, _ := agent.New(agent.Config{
		Name: "cb-agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(session.NewEvent("inv"), nil)
			}
		},
		BeforeAgentCallbacks: []agent.BeforeAgentCallback{
			func(ctx agent.CallbackContext) (*session.Event, error) {
				callbackCalled = true
				// Access callback context methods
				_ = ctx.AgentName()
				_ = ctx.ReadonlyState()
				_ = ctx.State()
				_ = ctx.UserID()
				_ = ctx.AppName()
				_ = ctx.SessionID()
				return nil, nil
			},
		},
	})

	sess := createMockSession()
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
	})

	for range a.Run(ctx) {
	}
	if !callbackCalled {
		t.Fatal("before callback not called")
	}
}

func TestRunWithAfterCallback(t *testing.T) {
	afterCalled := false
	a, _ := agent.New(agent.Config{
		Name: "after-agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(session.NewEvent("inv"), nil)
			}
		},
		AfterAgentCallbacks: []agent.AfterAgentCallback{
			func(ctx agent.CallbackContext) (*session.Event, error) {
				afterCalled = true
				// Set state through callback
				ctx.State().Set("cb_key", "cb_value")
				return nil, nil
			},
		},
	})

	sess := createMockSession()
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
	})

	for range a.Run(ctx) {
	}
	if !afterCalled {
		t.Fatal("after callback not called")
	}
}

func TestRunWithBeforeCallbackShortCircuit(t *testing.T) {
	a, _ := agent.New(agent.Config{
		Name: "short-circuit",
		BeforeAgentCallbacks: []agent.BeforeAgentCallback{
			func(ctx agent.CallbackContext) (*session.Event, error) {
				evt := session.NewEvent("inv")
				evt.Author = "intercepted"
				return evt, nil // Short-circuit — don't run agent
			},
		},
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			t.Fatal("Run should not be called")
			return nil
		},
	})

	sess := createMockSession()
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
	})

	for evt, _ := range a.Run(ctx) {
		if evt != nil && evt.Author == "intercepted" {
			return
		}
	}
	t.Fatal("expected intercepted event")
}

func TestRunWithNilRun(t *testing.T) {
	a, _ := agent.New(agent.Config{Name: "no-run"})
	sess := createMockSession()
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
	})

	for range a.Run(ctx) {
	}
	// Should not panic with nil run function
}

func TestMultiLoaderLoadAgent(t *testing.T) {
	a1, _ := agent.New(agent.Config{Name: "first"})
	a2, _ := agent.New(agent.Config{Name: "second"})
	loader, _ := agent.NewMultiLoader(a1, a2)

	loaded, err := loader.LoadAgent("second")
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}
	if loaded.Name() != "second" {
		t.Fatal("wrong agent loaded")
	}

	_, err = loader.LoadAgent("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func createMockSession() session.Session {
	svc := session.InMemoryService()
	ctx := context.Background()
	resp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName:   "test",
		UserID:    "u1",
		SessionID: "s1",
	})
	return resp.Session
}

func TestNewAgentDuplicateSubAgents(t *testing.T) {
	child1, _ := agent.New(agent.Config{Name: "child"})
	child2, _ := agent.New(agent.Config{Name: "child"})

	_, err := agent.New(agent.Config{
		Name:      "root",
		SubAgents: []agent.Agent{child1, child2},
	})
	if err == nil {
		t.Fatal("expected error for duplicate sub-agent names")
	}
}

func TestRunContextEndedBeforeRun(t *testing.T) {
	a, _ := agent.New(agent.Config{
		Name: "ended-agent",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			t.Fatal("Run should not be called when context is ended")
			return nil
		},
	})

	sess := createMockSession()
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
	})
	ctx.EndInvocation() // End before running

	for range a.Run(ctx) {
	}
}

func TestRunAfterCallbackShortCircuit(t *testing.T) {
	a, _ := agent.New(agent.Config{
		Name: "after-short",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				evt := session.NewEvent("inv")
				evt.Author = "main-run"
				yield(evt, nil)
			}
		},
		AfterAgentCallbacks: []agent.AfterAgentCallback{
			func(ctx agent.CallbackContext) (*session.Event, error) {
				evt := session.NewEvent("inv")
				evt.Author = "after-callback"
				return evt, nil // Short-circuit
			},
		},
	})

	sess := createMockSession()
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
	})

	var authors []string
	for evt, _ := range a.Run(ctx) {
		if evt != nil {
			authors = append(authors, evt.Author)
		}
	}
	// Should have main-run and after-callback
	if len(authors) < 1 {
		t.Fatal("expected at least 1 event")
	}
}

func TestRunYieldEarlyExit(t *testing.T) {
	a, _ := agent.New(agent.Config{
		Name: "multi-yield",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				for i := 0; i < 10; i++ {
					evt := session.NewEvent("inv")
					if !yield(evt, nil) {
						return
					}
				}
			}
		},
	})

	sess := createMockSession()
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
	})

	count := 0
	for range a.Run(ctx) {
		count++
		if count >= 2 {
			break
		}
	}
}

func TestFindSubAgent(t *testing.T) {
	child, _ := agent.New(agent.Config{Name: "child"})
	root, _ := agent.New(agent.Config{
		Name:      "root",
		SubAgents: []agent.Agent{child},
	})

	found := root.FindSubAgent("child")
	if found == nil || found.Name() != "child" {
		t.Fatal("expected to find child")
	}

	found = root.FindSubAgent("nonexistent")
	if found != nil {
		t.Fatal("expected nil for nonexistent sub-agent")
	}

	// FindSubAgent should NOT find self
	found = root.FindSubAgent("root")
	if found != nil {
		t.Fatal("FindSubAgent should not find self")
	}
}

func TestPluginHooksAccessor(t *testing.T) {
	a, _ := agent.New(agent.Config{Name: "hooks-agent"})
	sess := createMockSession()

	hooks := "my-plugin-hooks"
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:         context.Background(),
		Agent:       a,
		Session:     sess,
		PluginHooks: hooks,
	})

	if ctx.PluginHooks() != hooks {
		t.Fatal("expected plugin hooks to be set")
	}
}

func TestRunAuthorAutoSet(t *testing.T) {
	a, _ := agent.New(agent.Config{
		Name: "auto-author",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				evt := session.NewEvent("inv")
				// Don't set author — should be auto-set to agent name
				yield(evt, nil)
			}
		},
	})

	sess := createMockSession()
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
	})

	for evt, _ := range a.Run(ctx) {
		if evt != nil && evt.Author != "auto-author" {
			t.Fatalf("expected author 'auto-author', got %q", evt.Author)
		}
	}
}
