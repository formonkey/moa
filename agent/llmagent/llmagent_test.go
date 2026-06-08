package llmagent_test

import (
	"context"
	"errors"
	"iter"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/llmagent"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

func TestSimpleTextGeneration(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "Hello from LLM!"}

	a, err := llmagent.New(llmagent.Config{
		Name:        "test-llm",
		Description: "A test LLM agent",
		Model:       m,
		Instruction: "You are helpful",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	var events []*session.Event
	for evt, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		if evt != nil {
			events = append(events, evt)
		}
	}
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	if m.CallCount == 0 {
		t.Fatal("model never called")
	}
}

func TestWithInstructionProvider(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}
	providerCalled := false

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		InstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
			providerCalled = true
			return "Dynamic instruction", nil
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	if !providerCalled {
		t.Fatal("InstructionProvider not called")
	}
}

func TestWithGlobalInstruction(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, _ := llmagent.New(llmagent.Config{
		Name:              "test",
		Model:             m,
		Instruction:       "Agent instruction",
		GlobalInstruction: "Global prefix",
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
}

func TestWithGlobalInstructionProvider(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}
	called := false

	a, _ := llmagent.New(llmagent.Config{
		Name:        "test",
		Model:       m,
		Instruction: "base",
		GlobalInstructionProvider: func(ctx agent.ReadonlyContext) (string, error) {
			called = true
			return "Global dynamic", nil
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	if !called {
		t.Fatal("GlobalInstructionProvider not called")
	}
}

func TestWithToolExecution(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		FunctionCalls: []*genai.FunctionCall{{
			Name: "calculator",
			Args: map[string]any{"expression": "2+2"},
		}},
		Response: "The answer is 4",
	}

	calcTool := &mockRunnableTool{
		name: "calculator",
		decl: &genai.FunctionDeclaration{
			Name:        "calculator",
			Description: "Calculate",
			Parameters:  &genai.Schema{Type: "OBJECT", Properties: map[string]*genai.Schema{"expression": {Type: "STRING"}}},
		},
		exec: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{"result": 4}, nil
		},
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "calc-agent",
		Model: m,
		Tools: []tool.Tool{calcTool},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	eventCount := 0
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		eventCount++
	}
	if eventCount == 0 {
		t.Fatal("expected events")
	}
}

func TestToolError(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		FunctionCalls: []*genai.FunctionCall{{
			Name: "failing_tool",
			Args: map[string]any{},
		}},
		Response: "Tool failed, I'll retry",
	}

	failTool := &mockRunnableTool{
		name: "failing_tool",
		decl: &genai.FunctionDeclaration{Name: "failing_tool"},
		exec: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			return nil, errors.New("tool exploded")
		},
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		Tools: []tool.Tool{failTool},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
}

func TestModelError(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		Err:       errors.New("model unavailable"),
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	for _, err := range a.Run(ctx) {
		if err != nil {
			return // Expected
		}
	}
	t.Fatal("expected model error")
}

func TestModelErrorCallback(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		Err:       errors.New("model unavailable"),
	}
	callbackCalled := false

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		OnModelErrorCallbacks: []llmagent.OnModelErrorCallback{
			func(ctx agent.CallbackContext, req *model.LLMRequest, err error) (*model.LLMResponse, error) {
				callbackCalled = true
				return &model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "recovered"}},
					},
				}, nil
			},
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	if !callbackCalled {
		t.Fatal("OnModelError callback not called")
	}
}

func TestBeforeModelCallback(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{
			func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
				// Return cached response — skip model call
				return &model.LLMResponse{
					Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "cached"}}},
				}, nil
			},
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	if m.CallCount != 0 {
		t.Fatal("model should not have been called — cached response")
	}
}

func TestAfterModelCallback(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "original"}
	modified := false

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		AfterModelCallbacks: []llmagent.AfterModelCallback{
			func(ctx agent.CallbackContext, resp *model.LLMResponse, err error) (*model.LLMResponse, error) {
				modified = true
				return nil, nil // Don't modify
			},
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	if !modified {
		t.Fatal("AfterModel callback not called")
	}
}

func TestOutputKey(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "saved value"}

	a, _ := llmagent.New(llmagent.Config{
		Name:      "test",
		Model:     m,
		OutputKey: "result",
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	for evt, _ := range a.Run(ctx) {
		if evt != nil && evt.Actions.StateDelta != nil {
			if evt.Actions.StateDelta["result"] == "saved value" {
				return // Success
			}
		}
	}
	t.Fatal("expected outputKey in state delta")
}

func TestTransferToAgent(t *testing.T) {
	child := testutil.MockAgent("child-agent", "from child")
	m := &testutil.MockModel{
		ModelName: "mock",
		FunctionCalls: []*genai.FunctionCall{{
			Name: "transfer_to_agent",
			Args: map[string]any{"agent_name": "child-agent"},
		}},
		Response: "transferred",
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:      "parent",
		Model:     m,
		SubAgents: []agent.Agent{child},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)

	for evt, _ := range a.Run(ctx) {
		if evt != nil && evt.Actions.TransferToAgent == "child-agent" {
			return // Success
		}
	}
	t.Fatal("expected transfer action")
}

func TestFindAgent(t *testing.T) {
	child := testutil.MockAgent("child", "ok")
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, _ := llmagent.New(llmagent.Config{
		Name:      "parent",
		Model:     m,
		SubAgents: []agent.Agent{child},
	})

	if a.FindAgent("parent") != a {
		t.Fatal("FindAgent should find self")
	}
	if a.FindAgent("child") == nil {
		t.Fatal("FindAgent should find child")
	}
	if a.FindAgent("nonexistent") != nil {
		t.Fatal("FindAgent should return nil for nonexistent")
	}
}

func TestDisallowTransferToParent(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, _ := llmagent.New(llmagent.Config{
		Name:                     "test",
		Model:                    m,
		DisallowTransferToParent: true,
	})

	if la, ok := a.(agent.LLMAgentInternal); ok {
		if !la.DisallowTransferToParent() {
			t.Fatal("expected DisallowTransferToParent=true")
		}
	}
}

func TestStreamChunks(t *testing.T) {
	m := &testutil.MockModel{
		ModelName:    "mock",
		StreamChunks: []string{"chunk1", "chunk2", "chunk3"},
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
	})

	_, sess := testutil.NewTestSession(t)
	ctx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:     context.Background(),
		Agent:   a,
		Session: sess,
		RunConfig: &agent.RunConfig{StreamingMode: agent.StreamingModeSSE},
		UserContent: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: "test"}},
		},
	})

	count := 0
	for _, err := range a.Run(ctx) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected events")
	}
}

func TestIncludeContentsNone(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, _ := llmagent.New(llmagent.Config{
		Name:            "test",
		Model:           m,
		IncludeContents: llmagent.IncludeContentsNone,
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
}

func TestTemplateResolution(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, _ := llmagent.New(llmagent.Config{
		Name:        "test",
		Model:       m,
		Instruction: "Hello {name}, you are a {role}",
	})

	svc := session.InMemoryService()
	resp, _ := svc.Create(context.Background(), &session.CreateRequest{
		AppName: "test", UserID: "u", SessionID: "s",
	})
	sess := resp.Session
	sess.State().Set("name", "Alice")
	sess.State().Set("role", "engineer")

	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
}

func TestBeforeToolCallback(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		FunctionCalls: []*genai.FunctionCall{{Name: "test_tool", Args: map[string]any{}}},
		Response:  "ok",
	}
	callbackCalled := false

	testTool := &mockRunnableTool{
		name: "test_tool",
		decl: &genai.FunctionDeclaration{Name: "test_tool"},
		exec: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		Tools: []tool.Tool{testTool},
		BeforeToolCallbacks: []llmagent.BeforeToolCallback{
			func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
				callbackCalled = true
				return nil, nil
			},
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	if !callbackCalled {
		t.Fatal("BeforeTool callback not called")
	}
}

func TestAfterToolCallback(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		FunctionCalls: []*genai.FunctionCall{{Name: "test_tool", Args: map[string]any{}}},
		Response:  "ok",
	}
	callbackCalled := false

	testTool := &mockRunnableTool{
		name: "test_tool",
		decl: &genai.FunctionDeclaration{Name: "test_tool"},
		exec: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		Tools: []tool.Tool{testTool},
		AfterToolCallbacks: []llmagent.AfterToolCallback{
			func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any, err error) (map[string]any, error) {
				callbackCalled = true
				return nil, nil
			},
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	if !callbackCalled {
		t.Fatal("AfterTool callback not called")
	}
}

func TestOnToolErrorCallback(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		FunctionCalls: []*genai.FunctionCall{{Name: "broken_tool", Args: map[string]any{}}},
		Response:  "recovered",
	}
	callbackCalled := false

	brokenTool := &mockRunnableTool{
		name: "broken_tool",
		decl: &genai.FunctionDeclaration{Name: "broken_tool"},
		exec: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			return nil, errors.New("broken")
		},
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		Tools: []tool.Tool{brokenTool},
		OnToolErrorCallbacks: []llmagent.OnToolErrorCallback{
			func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
				callbackCalled = true
				return map[string]any{"recovered": true}, nil
			},
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	if !callbackCalled {
		t.Fatal("OnToolError callback not called")
	}
}

func TestToolNotFound(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		FunctionCalls: []*genai.FunctionCall{{Name: "nonexistent_tool", Args: map[string]any{}}},
		Response:  "ok",
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
	// Should not crash — error goes into function response
}

func TestWithToolsets(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	ts := tool.SimpleToolset("test-set", &mockRunnableTool{
		name: "set_tool",
		decl: &genai.FunctionDeclaration{Name: "set_tool"},
		exec: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{}, nil
		},
	})

	a, _ := llmagent.New(llmagent.Config{
		Name:     "test",
		Model:    m,
		Toolsets: []tool.Toolset{ts},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
}

func TestWithNativeTool(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: "ok"}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		Tools: []tool.Tool{&mockNativeTool{}},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
}

func TestOutputSchema(t *testing.T) {
	m := &testutil.MockModel{ModelName: "mock", Response: `{"answer": 42}`}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		GenerateContentConfig: &genai.GenerateContentConfig{},
		OutputSchema: &genai.Schema{
			Type: "OBJECT",
			Properties: map[string]*genai.Schema{
				"answer": {Type: "NUMBER"},
			},
		},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for range a.Run(ctx) {
	}
}

func TestEscalateFromTool(t *testing.T) {
	m := &testutil.MockModel{
		ModelName: "mock",
		FunctionCalls: []*genai.FunctionCall{{Name: "exit", Args: map[string]any{}}},
		Response:  "done",
	}

	exitTool := &mockRunnableTool{
		name: "exit",
		decl: &genai.FunctionDeclaration{Name: "exit"},
		exec: func(ctx context.Context, args map[string]any) (map[string]any, error) {
			return map[string]any{"_escalate": true, "reason": "done"}, nil
		},
	}

	a, _ := llmagent.New(llmagent.Config{
		Name:  "test",
		Model: m,
		Tools: []tool.Tool{exitTool},
	})

	_, sess := testutil.NewTestSession(t)
	ctx := testutil.NewTestContext(a, sess)
	for evt, _ := range a.Run(ctx) {
		if evt != nil && evt.Actions.Escalate {
			return // Success
		}
	}
	t.Fatal("expected escalate action")
}

// --- Mock helpers ---

type mockRunnableTool struct {
	name string
	decl *genai.FunctionDeclaration
	exec func(context.Context, map[string]any) (map[string]any, error)
}

func (t *mockRunnableTool) Name() string                        { return t.name }
func (t *mockRunnableTool) Description() string                 { return t.name }
func (t *mockRunnableTool) IsNative() bool                      { return false }
func (t *mockRunnableTool) IsLongRunning() bool                 { return false }
func (t *mockRunnableTool) Declaration() *genai.FunctionDeclaration { return t.decl }
func (t *mockRunnableTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return t.exec(ctx, args)
}

type mockNativeTool struct{}

func (t *mockNativeTool) Name() string        { return "native_mock" }
func (t *mockNativeTool) Description() string { return "mock native" }
func (t *mockNativeTool) IsNative() bool      { return true }
func (t *mockNativeTool) IsLongRunning() bool { return false }
func (t *mockNativeTool) ProcessRequest(req *model.LLMRequest) error {
	req.Tools = append(req.Tools, &genai.Tool{GoogleSearch: &genai.GoogleSearch{}})
	return nil
}

var _ tool.NativeTool = (*mockNativeTool)(nil)
var _ tool.RunnableTool = (*mockRunnableTool)(nil)

// Ensure we satisfy the iter.Seq2 type (compile check)
var _ iter.Seq2[*session.Event, error] = (func(yield func(*session.Event, error) bool))(nil)
