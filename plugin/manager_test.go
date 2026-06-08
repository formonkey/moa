package plugin_test

import (
	"errors"
	"testing"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

type mockCallbackCtx struct {
	agent.InvocationContext
}

func (m *mockCallbackCtx) AgentName() string                   { return "test-agent" }
func (m *mockCallbackCtx) ReadonlyState() session.ReadonlyState { return m.Session().State() }
func (m *mockCallbackCtx) State() session.State                 { return m.Session().State() }
func (m *mockCallbackCtx) UserID() string                       { return "u1" }
func (m *mockCallbackCtx) AppName() string                      { return "test" }
func (m *mockCallbackCtx) SessionID() string                    { return "s1" }

func newMockCtx(t *testing.T) agent.CallbackContext {
	t.Helper()
	a := testutil.MockAgent("test-agent", "ok")
	_, sess := testutil.NewTestSession(t)
	invCtx := testutil.NewTestContext(a, sess)
	return &mockCallbackCtx{InvocationContext: invCtx}
}

type mockTool struct{ name string }

func (t *mockTool) Name() string        { return t.name }
func (t *mockTool) Description() string { return "mock" }
func (t *mockTool) IsNative() bool      { return false }
func (t *mockTool) IsLongRunning() bool { return false }

func TestManagerBeforeAfterRun(t *testing.T) {
	var order []string

	p1 := &plugin.Plugin{
		Name:              "p1",
		BeforeRunCallback: func(ctx agent.CallbackContext) error { order = append(order, "before-p1"); return nil },
		AfterRunCallback:  func(ctx agent.CallbackContext) error { order = append(order, "after-p1"); return nil },
	}
	p2 := &plugin.Plugin{
		Name:              "p2",
		BeforeRunCallback: func(ctx agent.CallbackContext) error { order = append(order, "before-p2"); return nil },
		AfterRunCallback:  func(ctx agent.CallbackContext) error { order = append(order, "after-p2"); return nil },
	}

	mgr := plugin.NewManager(p1, p2)
	ctx := newMockCtx(t)

	if err := mgr.BeforeRun(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mgr.AfterRun(ctx); err != nil {
		t.Fatal(err)
	}
	if len(order) != 4 {
		t.Fatalf("expected 4 calls, got %d: %v", len(order), order)
	}
}

func TestManagerBeforeAfterAgent(t *testing.T) {
	called := false
	p := &plugin.Plugin{
		Name: "p",
		BeforeAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			called = true
			return nil, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)

	_, err := mgr.BeforeAgent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("BeforeAgent not called")
	}
}

func TestManagerBeforeAfterModel(t *testing.T) {
	called := false
	p := &plugin.Plugin{
		Name: "p",
		BeforeModelCallback: func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			called = true
			return nil, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)

	resp, err := mgr.BeforeModel(ctx, &model.LLMRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp != nil {
		t.Fatal("expected nil response (no short-circuit)")
	}
	if !called {
		t.Fatal("BeforeModel not called")
	}
}

func TestManagerBeforeAfterTool(t *testing.T) {
	var beforeCalled, afterCalled bool
	p := &plugin.Plugin{
		Name: "p",
		BeforeToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			beforeCalled = true
			return nil, nil
		},
		AfterToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error) {
			afterCalled = true
			return result, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	mt := &mockTool{name: "test"}

	_, err := mgr.BeforeTool(ctx, mt, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.AfterTool(ctx, mt, map[string]any{}, map[string]any{"result": "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if !beforeCalled || !afterCalled {
		t.Fatal("tool callbacks not called")
	}
}

func TestManagerOnToolError(t *testing.T) {
	p := &plugin.Plugin{
		Name: "p",
		OnToolErrorCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
			return map[string]any{"recovered": true}, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	mt := &mockTool{name: "test"}

	result, err := mgr.OnToolError(ctx, mt, map[string]any{}, errors.New("fail"))
	if err != nil {
		t.Fatal(err)
	}
	if result["recovered"] != true {
		t.Fatal("expected recovered result")
	}
}

func TestManagerOnEvent(t *testing.T) {
	p := &plugin.Plugin{
		Name: "p",
		OnEventCallback: func(ctx agent.CallbackContext, e *session.Event) (*session.Event, error) {
			e.Author = "modified"
			return e, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	evt := session.NewEvent("inv")
	evt.Author = "original"

	modified, err := mgr.OnEvent(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if modified.Author != "modified" {
		t.Fatal("event not modified")
	}
}

func TestManagerClose(t *testing.T) {
	closed := false
	p := &plugin.Plugin{
		Name:      "p",
		CloseFunc: func() error { closed = true; return nil },
	}
	mgr := plugin.NewManager(p)
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("Close not called")
	}
}

func TestManagerOnModelError(t *testing.T) {
	p := &plugin.Plugin{
		Name: "p",
		OnModelErrorCallback: func(ctx agent.CallbackContext, req *model.LLMRequest, err error) (*model.LLMResponse, error) {
			return nil, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	_, err := mgr.OnModelError(ctx, &model.LLMRequest{}, errors.New("model err"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestManagerOnUserMessage(t *testing.T) {
	p := &plugin.Plugin{
		Name: "p",
		OnUserMessageCallback: func(ctx agent.CallbackContext, e *session.Event) (*session.Event, error) {
			return e, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	evt := session.NewEvent("inv")
	_, err := mgr.OnUserMessage(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
}

func TestManagerNilCallbacks(t *testing.T) {
	p := &plugin.Plugin{Name: "empty"}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)

	mgr.BeforeRun(ctx)
	mgr.AfterRun(ctx)
	mgr.BeforeAgent(ctx)
	mgr.AfterAgent(ctx)
	mgr.BeforeModel(ctx, &model.LLMRequest{})
	mgr.AfterModel(ctx, &model.LLMResponse{})
	mgr.BeforeTool(ctx, &mockTool{}, nil)
	mgr.AfterTool(ctx, &mockTool{}, nil, nil)
	mgr.OnToolError(ctx, &mockTool{}, nil, errors.New("x"))
	mgr.OnModelError(ctx, &model.LLMRequest{}, errors.New("x"))
	mgr.OnEvent(ctx, session.NewEvent("inv"))
	mgr.OnUserMessage(ctx, session.NewEvent("inv"))
	mgr.Close()
}

func TestManagerBeforeRunError(t *testing.T) {
	p := &plugin.Plugin{
		Name:              "fail",
		BeforeRunCallback: func(ctx agent.CallbackContext) error { return errors.New("before-run failed") },
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	err := mgr.BeforeRun(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestManagerAfterRunError(t *testing.T) {
	p := &plugin.Plugin{
		Name:             "fail",
		AfterRunCallback: func(ctx agent.CallbackContext) error { return errors.New("after-run failed") },
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	err := mgr.AfterRun(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestManagerBeforeAgentShortCircuit(t *testing.T) {
	evt := session.NewEvent("inv")
	p := &plugin.Plugin{
		Name: "short",
		BeforeAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			return evt, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	result, err := mgr.BeforeAgent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil short-circuit event")
	}
}

func TestManagerAfterAgentShortCircuit(t *testing.T) {
	evt := session.NewEvent("inv")
	p := &plugin.Plugin{
		Name: "short",
		AfterAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			return evt, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	result, err := mgr.AfterAgent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil short-circuit event")
	}
}

func TestManagerBeforeModelShortCircuit(t *testing.T) {
	resp := &model.LLMResponse{}
	p := &plugin.Plugin{
		Name: "short",
		BeforeModelCallback: func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			return resp, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	result, err := mgr.BeforeModel(ctx, &model.LLMRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil short-circuit response")
	}
}

func TestManagerAfterModelShortCircuit(t *testing.T) {
	resp := &model.LLMResponse{}
	p := &plugin.Plugin{
		Name: "short",
		AfterModelCallback: func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
			return resp, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	result, err := mgr.AfterModel(ctx, resp)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil short-circuit response")
	}
}

func TestManagerBeforeToolShortCircuit(t *testing.T) {
	p := &plugin.Plugin{
		Name: "short",
		BeforeToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			return map[string]any{"intercepted": true}, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	result, _ := mgr.BeforeTool(ctx, &mockTool{}, nil)
	if result == nil || result["intercepted"] != true {
		t.Fatal("expected intercepted result")
	}
}

func TestManagerAfterToolShortCircuit(t *testing.T) {
	p := &plugin.Plugin{
		Name: "short",
		AfterToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error) {
			return map[string]any{"modified": true}, nil
		},
	}
	mgr := plugin.NewManager(p)
	ctx := newMockCtx(t)
	result, _ := mgr.AfterTool(ctx, &mockTool{}, nil, nil)
	if result == nil || result["modified"] != true {
		t.Fatal("expected modified result")
	}
}

func TestManagerCloseError(t *testing.T) {
	p := &plugin.Plugin{
		Name:      "fail",
		CloseFunc: func() error { return errors.New("close failed") },
	}
	mgr := plugin.NewManager(p)
	err := mgr.Close()
	if err == nil {
		t.Fatal("expected error from Close")
	}
}
