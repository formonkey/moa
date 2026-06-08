package session_test

import (
	"context"
	"testing"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"google.golang.org/genai"
)

func newTestService(t *testing.T) session.Service {
	t.Helper()
	return session.InMemoryService()
}

func TestCreateAndGetSession(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, err := svc.Create(ctx, &session.CreateRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if createResp.Session == nil {
		t.Fatal("expected non-nil session")
	}
	if createResp.Session.ID() != "sess-1" {
		t.Fatalf("expected session ID 'sess-1', got %q", createResp.Session.ID())
	}
	if createResp.Session.UserID() != "user-1" {
		t.Fatalf("expected user ID 'user-1', got %q", createResp.Session.UserID())
	}

	// Get the session back
	getResp, err := svc.Get(ctx, &session.GetRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if getResp.Session.ID() != "sess-1" {
		t.Fatalf("Get returned wrong session ID: %q", getResp.Session.ID())
	}
}

func TestGetSessionNotFound(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Get(ctx, &session.GetRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestAppendAndRetrieveEvents(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-events",
	})
	sess := createResp.Session

	// Append a user event
	userEvent := session.NewEvent("inv-1")
	userEvent.Author = "user"
	userEvent.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: "Hello"}},
		},
	}
	if err := svc.AppendEvent(ctx, sess, userEvent); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Append a model event
	modelEvent := session.NewEvent("inv-1")
	modelEvent.Author = "test-agent"
	modelEvent.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: "Hi there!"}},
		},
	}
	if err := svc.AppendEvent(ctx, sess, modelEvent); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Verify events are retrievable
	getResp, _ := svc.Get(ctx, &session.GetRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-events",
	})
	events := getResp.Session.Events()
	if events.Len() != 2 {
		t.Fatalf("expected 2 events, got %d", events.Len())
	}
	if events.At(0).Author != "user" {
		t.Fatalf("first event should be from 'user', got %q", events.At(0).Author)
	}
	if events.At(1).Author != "test-agent" {
		t.Fatalf("second event should be from 'test-agent', got %q", events.At(1).Author)
	}
}

func TestSessionState(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-state",
	})
	sess := createResp.Session

	// Set and get state
	sess.State().Set("key1", "value1")
	sess.State().Set("key2", 42)

	val1, err := sess.State().Get("key1")
	if err != nil {
		t.Fatalf("State.Get failed: %v", err)
	}
	if val1 != "value1" {
		t.Fatalf("expected 'value1', got %v", val1)
	}

	val2, err := sess.State().Get("key2")
	if err != nil {
		t.Fatalf("State.Get failed: %v", err)
	}
	if val2 != 42 {
		t.Fatalf("expected 42, got %v", val2)
	}

	// Test nonexistent key
	_, err = sess.State().Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

func TestStateDeltaViaEvent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-delta",
	})
	sess := createResp.Session

	// Append an event with state delta
	event := session.NewEvent("inv-1")
	event.Author = "agent"
	event.Actions.StateDelta = map[string]any{
		"computed_result": "hello-world",
	}
	if err := svc.AppendEvent(ctx, sess, event); err != nil {
		t.Fatalf("AppendEvent with delta failed: %v", err)
	}

	// The state delta should be applied
	val, err := sess.State().Get("computed_result")
	if err != nil {
		t.Fatalf("State.Get after delta failed: %v", err)
	}
	if val != "hello-world" {
		t.Fatalf("expected 'hello-world', got %v", val)
	}
}

func TestDeleteSession(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.Create(ctx, &session.CreateRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-delete",
	})

	err := svc.Delete(ctx, &session.DeleteRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-delete",
	})
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Should not be findable after delete
	_, err = svc.Get(ctx, &session.GetRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "sess-delete",
	})
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestListSessions(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "u1", SessionID: "s1"})
	svc.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "u1", SessionID: "s2"})
	svc.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "u2", SessionID: "s3"})

	resp, err := svc.List(ctx, &session.ListRequest{AppName: "app", UserID: "u1"})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions for u1, got %d", len(resp.Sessions))
	}
}

func TestSessionLastUpdateTime(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s-time",
	})
	sess := createResp.Session

	t1 := sess.LastUpdateTime()
	if t1.IsZero() {
		t.Fatal("expected non-zero LastUpdateTime after creation")
	}

	// Append event should update time
	event := session.NewEvent("inv-1")
	event.Author = "agent"
	event.LLMResponse = model.LLMResponse{Content: &genai.Content{
		Role: "model", Parts: []*genai.Part{{Text: "hi"}},
	}}
	svc.AppendEvent(ctx, sess, event)

	t2 := sess.LastUpdateTime()
	if t2.Before(t1) {
		t.Fatal("expected LastUpdateTime to advance after AppendEvent")
	}
}

func TestEventsAll(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s-all",
	})
	sess := createResp.Session

	for i := range 3 {
		event := session.NewEvent("inv-1")
		event.Author = "agent"
		event.LLMResponse = model.LLMResponse{Content: &genai.Content{
			Role: "model", Parts: []*genai.Part{{Text: "msg"}},
		}}
		_ = i
		svc.AppendEvent(ctx, sess, event)
	}

	count := 0
	for e := range sess.Events().All() {
		if e == nil {
			t.Fatal("nil event in All()")
		}
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 events, got %d", count)
	}
}

func TestEventsAtOutOfBounds(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s-oob",
	})
	sess := createResp.Session

	// No events
	if sess.Events().At(0) != nil {
		t.Fatal("expected nil for out-of-bounds At(0)")
	}
	if sess.Events().At(-1) != nil {
		t.Fatal("expected nil for At(-1)")
	}
}

func TestStateAll(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s-state-all",
	})
	sess := createResp.Session

	sess.State().Set("a", 1)
	sess.State().Set("b", 2)

	count := 0
	for k, v := range sess.State().All() {
		if k == "" || v == nil {
			t.Fatal("empty key or nil value")
		}
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 state entries, got %d", count)
	}
}

func TestCreateValidationErrors(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Empty AppName
	_, err := svc.Create(ctx, &session.CreateRequest{UserID: "u1"})
	if err == nil {
		t.Fatal("expected error for empty AppName")
	}

	// Empty UserID
	_, err = svc.Create(ctx, &session.CreateRequest{AppName: "app"})
	if err == nil {
		t.Fatal("expected error for empty UserID")
	}
}

func TestCreateAutoGeneratedSessionID(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	resp, err := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.Session.ID() == "" {
		t.Fatal("expected auto-generated session ID")
	}
}

func TestCreateDuplicateSession(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "u1", SessionID: "dup"})
	_, err := svc.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "u1", SessionID: "dup"})
	if err == nil {
		t.Fatal("expected error for duplicate session")
	}
}

func TestGetValidationErrors(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.Get(ctx, &session.GetRequest{})
	if err == nil {
		t.Fatal("expected error for empty Get request")
	}
}

func TestListValidationErrors(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.List(ctx, &session.ListRequest{})
	if err == nil {
		t.Fatal("expected error for empty AppName in List")
	}
}

func TestListAllUsers(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	svc.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "u1", SessionID: "s1"})
	svc.Create(ctx, &session.CreateRequest{AppName: "app", UserID: "u2", SessionID: "s2"})

	resp, err := svc.List(ctx, &session.ListRequest{AppName: "app"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
	}
}

func TestDeleteValidationErrors(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	err := svc.Delete(ctx, &session.DeleteRequest{})
	if err == nil {
		t.Fatal("expected error for empty Delete request")
	}
}

func TestAppendEventNilSession(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	event := session.NewEvent("inv-1")
	err := svc.AppendEvent(ctx, nil, event)
	if err == nil {
		t.Fatal("expected error for nil session")
	}
}

func TestAppendEventNilEvent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s1",
	})
	err := svc.AppendEvent(ctx, createResp.Session, nil)
	if err == nil {
		t.Fatal("expected error for nil event")
	}
}

func TestAppendEventPartialSkipped(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s1",
	})
	sess := createResp.Session

	event := session.NewEvent("inv-1")
	event.Partial = true
	err := svc.AppendEvent(ctx, sess, event)
	if err != nil {
		t.Fatalf("expected no error for partial event, got: %v", err)
	}

	// Verify no events were stored
	if sess.Events().Len() != 0 {
		t.Fatal("expected 0 events after partial")
	}
}

func TestAppendEventSessionNotFound(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Create a session, delete it, then try to append
	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "gone",
	})
	sess := createResp.Session
	svc.Delete(ctx, &session.DeleteRequest{AppName: "app", UserID: "u1", SessionID: "gone"})

	event := session.NewEvent("inv-1")
	event.Author = "agent"
	err := svc.AppendEvent(ctx, sess, event)
	if err == nil {
		t.Fatal("expected error for deleted session")
	}
}

func TestGetWithNumRecentEvents(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s-recent",
	})
	sess := createResp.Session

	for i := 0; i < 5; i++ {
		event := session.NewEvent("inv-1")
		event.Author = "agent"
		event.LLMResponse = model.LLMResponse{Content: &genai.Content{
			Role: "model", Parts: []*genai.Part{{Text: "msg"}},
		}}
		svc.AppendEvent(ctx, sess, event)
	}

	resp, _ := svc.Get(ctx, &session.GetRequest{
		AppName: "app", UserID: "u1", SessionID: "s-recent",
		NumRecentEvents: 2,
	})
	if resp.Session.Events().Len() != 2 {
		t.Fatalf("expected 2 recent events, got %d", resp.Session.Events().Len())
	}
}

func TestCreateWithInitialState(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	createResp, _ := svc.Create(ctx, &session.CreateRequest{
		AppName:   "app",
		UserID:    "u1",
		SessionID: "s-init-state",
		State:     map[string]any{"key": "val"},
	})

	val, err := createResp.Session.State().Get("key")
	if err != nil {
		t.Fatalf("State.Get: %v", err)
	}
	if val != "val" {
		t.Fatalf("expected 'val', got %v", val)
	}
}

func TestAppScopedState(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Create two sessions for the same app
	resp1, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s1",
	})
	svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u2", SessionID: "s2",
	})

	// Set app-scoped state via event
	event := session.NewEvent("inv-1")
	event.Author = "agent"
	event.Actions.StateDelta = map[string]any{
		"app:shared_key": "shared_val",
	}
	svc.AppendEvent(ctx, resp1.Session, event)

	// Get session 2 — should see app-scoped state
	resp2, _ := svc.Get(ctx, &session.GetRequest{
		AppName: "app", UserID: "u2", SessionID: "s2",
	})
	val, err := resp2.Session.State().Get("app:shared_key")
	if err != nil {
		t.Fatalf("expected app-scoped state, got error: %v", err)
	}
	if val != "shared_val" {
		t.Fatalf("expected 'shared_val', got %v", val)
	}
}

func TestUserScopedState(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	resp1, _ := svc.Create(ctx, &session.CreateRequest{
		AppName: "app", UserID: "u1", SessionID: "s1",
	})

	// Set user-scoped state
	event := session.NewEvent("inv-1")
	event.Author = "agent"
	event.Actions.StateDelta = map[string]any{
		"user:pref": "dark",
	}
	svc.AppendEvent(ctx, resp1.Session, event)

	// Get same user's session — should see user-scoped state
	getResp, _ := svc.Get(ctx, &session.GetRequest{
		AppName: "app", UserID: "u1", SessionID: "s1",
	})
	val, err := getResp.Session.State().Get("user:pref")
	if err != nil {
		t.Fatalf("expected user-scoped state: %v", err)
	}
	if val != "dark" {
		t.Fatalf("expected 'dark', got %v", val)
	}
}

