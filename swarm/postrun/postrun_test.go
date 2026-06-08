package postrun_test

import (
	"context"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/postrun"
	"github.com/formonkey/moa/swarm/scheduler"
)

type prCtx struct {
	context.Context
	sess session.Session
}

func (m *prCtx) UserContent() *genai.Content            { return nil }
func (m *prCtx) InvocationID() string                    { return "inv-1" }
func (m *prCtx) AgentName() string                       { return "worker" }
func (m *prCtx) ReadonlyState() session.ReadonlyState    { return m.sess.State() }
func (m *prCtx) UserID() string                          { return "u1" }
func (m *prCtx) AppName() string                         { return "app" }
func (m *prCtx) SessionID() string                       { return "s1" }
func (m *prCtx) Branch() string                          { return "" }
func (m *prCtx) Artifacts() agent.Artifacts              { return nil }
func (m *prCtx) State() session.State                    { return m.sess.State() }

var _ agent.CallbackContext = (*prCtx)(nil)

func TestPostRunCallback(t *testing.T) {
	bus := scheduler.NewEventBus()
	ch := bus.Subscribe("TASK_COMPLETED")

	svc := session.InMemoryService()
	resp, _ := svc.Create(context.Background(), &session.CreateRequest{AppName: "test", UserID: "u1", SessionID: "s1"})
	ctx := &prCtx{Context: context.Background(), sess: resp.Session}

	cb := postrun.NewCallback(bus)
	evt, err := cb(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = evt

	select {
	case e := <-ch:
		if e.Type != "TASK_COMPLETED" {
			t.Fatalf("wrong event: %s", e.Type)
		}
		if e.Source != "worker" {
			t.Fatalf("wrong source: %s", e.Source)
		}
	default:
		t.Fatal("expected TASK_COMPLETED event")
	}
}
