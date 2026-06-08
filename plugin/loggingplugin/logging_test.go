package loggingplugin_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin/loggingplugin"
	"github.com/formonkey/moa/session"
)

type mockCbCtx struct {
	context.Context
	sess session.Session
}

func (m *mockCbCtx) UserContent() *genai.Content            { return nil }
func (m *mockCbCtx) InvocationID() string                    { return "inv-1" }
func (m *mockCbCtx) AgentName() string                       { return "test-agent" }
func (m *mockCbCtx) ReadonlyState() session.ReadonlyState    { return m.sess.State() }
func (m *mockCbCtx) UserID() string                          { return "u1" }
func (m *mockCbCtx) AppName() string                         { return "app" }
func (m *mockCbCtx) SessionID() string                       { return "s1" }
func (m *mockCbCtx) Branch() string                          { return "" }
func (m *mockCbCtx) Artifacts() agent.Artifacts              { return nil }
func (m *mockCbCtx) State() session.State                    { return m.sess.State() }

var _ agent.CallbackContext = (*mockCbCtx)(nil)

func newCbCtx() *mockCbCtx {
	svc := session.InMemoryService()
	resp, _ := svc.Create(context.Background(), &session.CreateRequest{AppName: "test", UserID: "u1", SessionID: "s1"})
	return &mockCbCtx{Context: context.Background(), sess: resp.Session}
}

type dummyTool struct{}

func (d *dummyTool) Name() string        { return "dummy" }
func (d *dummyTool) Description() string { return "dummy tool" }
func (d *dummyTool) IsNative() bool      { return false }
func (d *dummyTool) IsLongRunning() bool { return false }

func TestLoggingPluginAllCallbacks(t *testing.T) {
	p := loggingplugin.New()
	if p.Name != "logging" {
		t.Fatal("wrong name")
	}

	ctx := newCbCtx()

	if p.BeforeRunCallback != nil {
		if err := p.BeforeRunCallback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if p.AfterRunCallback != nil {
		if err := p.AfterRunCallback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if p.BeforeAgentCallback != nil {
		evt, err := p.BeforeAgentCallback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_ = evt
	}
	if p.AfterAgentCallback != nil {
		evt, err := p.AfterAgentCallback(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_ = evt
	}
	if p.BeforeModelCallback != nil {
		req := &model.LLMRequest{
			Tools: []*genai.Tool{{
				FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "test"}},
			}},
		}
		resp, err := p.BeforeModelCallback(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp
	}
	if p.AfterModelCallback != nil {
		resp := &model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{Text: "ok"}}}}
		newResp, err := p.AfterModelCallback(ctx, resp)
		if err != nil {
			t.Fatal(err)
		}
		_ = newResp
	}
	if p.BeforeToolCallback != nil {
		result, err := p.BeforeToolCallback(ctx, &dummyTool{}, nil)
		_ = result
		_ = err
	}
	if p.AfterToolCallback != nil {
		result, err := p.AfterToolCallback(ctx, &dummyTool{}, nil, nil)
		_ = result
		_ = err
	}
	if p.OnModelErrorCallback != nil {
		_, err := p.OnModelErrorCallback(ctx, &model.LLMRequest{}, errors.New("model error"))
		if err != nil {
			t.Fatal(err)
		}
	}
	if p.OnToolErrorCallback != nil {
		_, err := p.OnToolErrorCallback(ctx, &dummyTool{}, nil, errors.New("tool error"))
		if err != nil {
			t.Fatal(err)
		}
	}
}
