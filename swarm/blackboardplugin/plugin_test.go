package blackboardplugin_test

import (
	"context"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/blackboard"
	"github.com/formonkey/moa/swarm/blackboardplugin"
)

type bbCtx struct {
	context.Context
	sess session.Session
}

func (m *bbCtx) UserContent() *genai.Content            { return nil }
func (m *bbCtx) InvocationID() string                    { return "inv-1" }
func (m *bbCtx) AgentName() string                       { return "coder" }
func (m *bbCtx) ReadonlyState() session.ReadonlyState    { return m.sess.State() }
func (m *bbCtx) UserID() string                          { return "u1" }
func (m *bbCtx) AppName() string                         { return "app" }
func (m *bbCtx) SessionID() string                       { return "s1" }
func (m *bbCtx) Branch() string                          { return "" }
func (m *bbCtx) Artifacts() agent.Artifacts              { return nil }
func (m *bbCtx) State() session.State                    { return m.sess.State() }

var _ agent.CallbackContext = (*bbCtx)(nil)

func newBBCtx() *bbCtx {
	svc := session.InMemoryService()
	resp, _ := svc.Create(context.Background(), &session.CreateRequest{AppName: "test", UserID: "u1", SessionID: "s1"})
	return &bbCtx{Context: context.Background(), sess: resp.Session}
}

func TestInjectorEmpty(t *testing.T) {
	bb := blackboard.New()
	cb := blackboardplugin.NewInjector(bb)

	ctx := newBBCtx()
	evt, err := cb(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if evt != nil {
		t.Fatal("expected nil for empty blackboard")
	}
}

func TestInjectorWithData(t *testing.T) {
	bb := blackboard.New()
	bb.Write("coder:result", "code output")
	cb := blackboardplugin.NewInjector(bb)

	ctx := newBBCtx()
	evt, err := cb(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = evt

	val, _ := ctx.sess.State().Get("blackboard_context")
	if val == nil {
		t.Fatal("expected blackboard_context in state")
	}
}

func TestPublisher(t *testing.T) {
	bb := blackboard.New()
	cb := blackboardplugin.NewPublisher(bb)

	ctx := newBBCtx()
	ctx.sess.State().Set("output", "my result")

	evt, err := cb(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = evt

	snap := bb.Snapshot()
	if snap["coder:result"] == nil {
		t.Fatal("expected published result")
	}
}

func TestPublisherNoOutput(t *testing.T) {
	bb := blackboard.New()
	cb := blackboardplugin.NewPublisher(bb)

	ctx := newBBCtx()
	evt, err := cb(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if evt != nil {
		t.Fatal("expected nil")
	}
}
