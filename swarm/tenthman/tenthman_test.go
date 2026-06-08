package tenthman_test

import (
	"context"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/blackboard"
	"github.com/formonkey/moa/swarm/scheduler"
	"github.com/formonkey/moa/swarm/tenthman"
)

type mockCbCtx struct {
	context.Context
	sess session.Session
}

func (m *mockCbCtx) UserContent() *genai.Content            { return nil }
func (m *mockCbCtx) InvocationID() string                    { return "inv-1" }
func (m *mockCbCtx) AgentName() string                       { return "auditor" }
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

func TestNewCallbackNoArtifacts(t *testing.T) {
	bb := blackboard.New()
	bus := scheduler.NewEventBus()
	cb := tenthman.NewCallback(bb, bus)

	ctx := newCbCtx()
	evt, err := cb(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt != nil {
		t.Fatal("expected nil event when no artifacts to review")
	}
}

func TestPostAuditCallbackNoMode(t *testing.T) {
	bus := scheduler.NewEventBus()
	cb := tenthman.PostAuditCallback(bus)

	ctx := newCbCtx()
	evt, err := cb(ctx)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if evt != nil {
		t.Fatal("expected nil when not in tenth_man_mode")
	}
}

func TestPostAuditCallbackPass(t *testing.T) {
	bus := scheduler.NewEventBus()
	cb := tenthman.PostAuditCallback(bus)

	ctx := newCbCtx()
	ctx.sess.State().Set("tenth_man_mode", true)
	ctx.sess.State().Set("tenth_man_artifact_author", "coder")
	ctx.sess.State().Set("tenth_man_verdict", "PASS")

	evt, err := cb(ctx)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	_ = evt
}

func TestPostAuditCallbackFail(t *testing.T) {
	bus := scheduler.NewEventBus()
	cb := tenthman.PostAuditCallback(bus)

	ctx := newCbCtx()
	ctx.sess.State().Set("tenth_man_mode", true)
	ctx.sess.State().Set("tenth_man_artifact_author", "coder")
	ctx.sess.State().Set("tenth_man_verdict", "FAIL")

	evt, err := cb(ctx)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	_ = evt
}
