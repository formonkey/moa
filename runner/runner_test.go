package runner_test

import (
	"context"
	"iter"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/session"
)

func TestRunnerBasic(t *testing.T) {
	mockAgent := testutil.MockAgent("root", "Hello from runner!")
	sessSvc := session.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:           "test",
		Agent:             mockAgent,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer r.Close()

	ctx := context.Background()
	msg := genai.NewContentFromText("hello", "user")

	count := 0
	for _, err := range r.Run(ctx, "user1", "sess1", msg, agent.RunConfig{}) {
		if err != nil {
			t.Fatalf("Run error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected at least 1 event")
	}
}

func TestRunnerRequiresAgent(t *testing.T) {
	_, err := runner.New(runner.Config{
		SessionService: session.InMemoryService(),
	})
	if err == nil {
		t.Fatal("expected error without agent")
	}
}

func TestRunnerRequiresSessionService(t *testing.T) {
	a := testutil.MockAgent("a", "x")
	_, err := runner.New(runner.Config{Agent: a})
	if err == nil {
		t.Fatal("expected error without session service")
	}
}

func TestRunnerSessionNotFound(t *testing.T) {
	a := testutil.MockAgent("a", "x")
	sessSvc := session.InMemoryService()

	r, _ := runner.New(runner.Config{
		AppName:           "test",
		Agent:             a,
		SessionService:    sessSvc,
		AutoCreateSession: false,
	})

	ctx := context.Background()
	msg := genai.NewContentFromText("hello", "user")

	for _, err := range r.Run(ctx, "user1", "nonexistent", msg, agent.RunConfig{}) {
		if err != nil {
			return
		}
	}
	t.Fatal("expected session not found error")
}

func TestRunnerWithPlugins(t *testing.T) {
	a := testutil.MockAgent("root", "plugged")
	sessSvc := session.InMemoryService()

	var beforeRan, afterRan bool
	p := &plugin.Plugin{
		Name: "test-plugin",
		BeforeRunCallback: func(ctx agent.CallbackContext) error {
			beforeRan = true
			return nil
		},
		AfterRunCallback: func(ctx agent.CallbackContext) error {
			afterRan = true
			return nil
		},
	}

	r, _ := runner.New(runner.Config{
		AppName:           "test",
		Agent:             a,
		SessionService:    sessSvc,
		Plugins:           []*plugin.Plugin{p},
		AutoCreateSession: true,
	})

	ctx := context.Background()
	for range r.Run(ctx, "u1", "s1", genai.NewContentFromText("hi", "user"), agent.RunConfig{}) {
	}

	if !beforeRan {
		t.Fatal("BeforeRun not called")
	}
	if !afterRan {
		t.Fatal("AfterRun not called")
	}
}

func TestRunnerWithStateDelta(t *testing.T) {
	a := testutil.MockAgent("root", "ok")
	sessSvc := session.InMemoryService()

	r, _ := runner.New(runner.Config{
		AppName:           "test",
		Agent:             a,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})

	ctx := context.Background()
	for range r.Run(ctx, "u1", "s1", genai.NewContentFromText("hi", "user"), agent.RunConfig{}, runner.WithStateDelta(map[string]any{"key": "val"})) {
	}
}

func TestRunnerWithBlobInput(t *testing.T) {
	a := testutil.MockAgent("root", "ok")
	sessSvc := session.InMemoryService()

	r, _ := runner.New(runner.Config{
		AppName:           "test",
		Agent:             a,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})

	ctx := context.Background()
	msg := &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			{InlineData: &genai.Blob{MIMEType: "image/png", Data: []byte("fakepng")}},
		},
	}
	// Without artifact service, blobs are not saved but shouldn't crash
	for range r.Run(ctx, "u1", "s1", msg, agent.RunConfig{}) {
	}
}

func TestRunnerWithTransfer(t *testing.T) {
	child := testutil.MockAgent("child", "from child")
	parent, _ := agent.New(agent.Config{
		Name:      "root",
		SubAgents: []agent.Agent{child},
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				evt := session.NewEvent(ctx.InvocationID())
				evt.Author = "root"
				evt.Actions.TransferToAgent = "child"
				evt.LLMResponse = model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "transferring"}},
					},
				}
				yield(evt, nil)
			}
		},
	})

	sessSvc := session.InMemoryService()
	r, _ := runner.New(runner.Config{
		AppName:           "test",
		Agent:             parent,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})

	ctx := context.Background()
	for range r.Run(ctx, "u1", "s1", genai.NewContentFromText("hi", "user"), agent.RunConfig{}) {
	}
	for range r.Run(ctx, "u1", "s1", genai.NewContentFromText("continue", "user"), agent.RunConfig{}) {
	}
}

func TestRunnerClose(t *testing.T) {
	a := testutil.MockAgent("root", "ok")
	sessSvc := session.InMemoryService()

	r, _ := runner.New(runner.Config{
		AppName:           "test",
		Agent:             a,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})

	// Close should be idempotent
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestRunnerPluginRunCallbacksWithContext(t *testing.T) {
	var agentName, userID, appName, sessID string
	p := &plugin.Plugin{
		Name: "ctx-hooks",
		BeforeRunCallback: func(ctx agent.CallbackContext) error {
			agentName = ctx.AgentName()
			userID = ctx.UserID()
			appName = ctx.AppName()
			sessID = ctx.SessionID()
			_ = ctx.ReadonlyState()
			_ = ctx.State()
			return nil
		},
	}

	a := testutil.MockAgent("root", "plugged-agent")
	sessSvc := session.InMemoryService()

	r, _ := runner.New(runner.Config{
		AppName:           "test-app",
		Agent:             a,
		SessionService:    sessSvc,
		Plugins:           []*plugin.Plugin{p},
		AutoCreateSession: true,
	})

	for range r.Run(context.Background(), "u1", "s1", genai.NewContentFromText("hi", "user"), agent.RunConfig{}) {
	}

	if agentName == "" {
		t.Fatal("AgentName not set")
	}
	if userID == "" {
		t.Fatal("UserID not set")
	}
	if appName == "" {
		t.Fatal("AppName not set")
	}
	if sessID == "" {
		t.Fatal("SessionID not set")
	}
}

func TestRunnerWithAppName(t *testing.T) {
	a := testutil.MockAgent("root", "ok")
	sessSvc := session.InMemoryService()

	r, _ := runner.New(runner.Config{
		AppName:           "my-cool-app",
		Agent:             a,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})

	for range r.Run(context.Background(), "u1", "s1", genai.NewContentFromText("hi", "user"), agent.RunConfig{}) {
	}
	r.Close()
}

