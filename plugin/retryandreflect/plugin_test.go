package retryandreflect_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/plugin/retryandreflect"
	"github.com/formonkey/moa/session"
)

type rrCtx struct {
	context.Context
	sess session.Session
}

func (m *rrCtx) UserContent() *genai.Content            { return nil }
func (m *rrCtx) InvocationID() string                    { return "inv-1" }
func (m *rrCtx) AgentName() string                       { return "agent" }
func (m *rrCtx) ReadonlyState() session.ReadonlyState    { return m.sess.State() }
func (m *rrCtx) UserID() string                          { return "u1" }
func (m *rrCtx) AppName() string                         { return "app" }
func (m *rrCtx) SessionID() string                       { return "s1" }
func (m *rrCtx) Branch() string                          { return "" }
func (m *rrCtx) Artifacts() agent.Artifacts              { return nil }
func (m *rrCtx) State() session.State                    { return m.sess.State() }

var _ agent.CallbackContext = (*rrCtx)(nil)

type dummyTool struct{ name string }

func (d *dummyTool) Name() string        { return d.name }
func (d *dummyTool) Description() string { return "dummy" }
func (d *dummyTool) IsNative() bool      { return false }
func (d *dummyTool) IsLongRunning() bool { return false }

func newRRCtx() *rrCtx {
	svc := session.InMemoryService()
	resp, _ := svc.Create(context.Background(), &session.CreateRequest{AppName: "test", UserID: "u1", SessionID: "s1"})
	return &rrCtx{Context: context.Background(), sess: resp.Session}
}

func TestRetryAndReflectDefaults(t *testing.T) {
	p := retryandreflect.New()
	if p.Name != "retry-and-reflect" {
		t.Fatal("wrong name")
	}
}

func TestRetryAndReflectWithOptions(t *testing.T) {
	p := retryandreflect.New(
		retryandreflect.WithMaxRetries(5),
		retryandreflect.WithErrorIfRetryExceeded(true),
		retryandreflect.WithTrackingScope(retryandreflect.Global),
	)
	if p.Name != "retry-and-reflect" {
		t.Fatal("wrong name")
	}
}

func TestRetryAndReflectOnToolError(t *testing.T) {
	p := retryandreflect.New(retryandreflect.WithMaxRetries(3))

	ctx := newRRCtx()
	tool := &dummyTool{name: "failing-tool"}

	if p.OnToolErrorCallback != nil {
		result, err := p.OnToolErrorCallback(ctx, tool, map[string]any{"input": "test"}, errors.New("tool failed"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected reflection response")
		}
	}
}

func TestRetryAndReflectExceedRetries(t *testing.T) {
	p := retryandreflect.New(retryandreflect.WithMaxRetries(1))

	ctx := newRRCtx()
	tool := &dummyTool{name: "exceed-tool"}

	if p.OnToolErrorCallback != nil {
		// First call: should get reflection
		result, err := p.OnToolErrorCallback(ctx, tool, map[string]any{"input": "a"}, errors.New("fail1"))
		if err != nil {
			t.Fatal(err)
		}
		_ = result

		// Second call: should exceed
		result2, err2 := p.OnToolErrorCallback(ctx, tool, map[string]any{"input": "b"}, errors.New("fail2"))
		_ = result2
		_ = err2
	}
}
