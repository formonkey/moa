package a2a_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/formonkey/moa/a2a"
	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/session"
)

func testCard() a2a.AgentCard {
	return a2a.AgentCard{
		Name:        "test-agent",
		Description: "A test agent",
		Version:     "1.0.0",
		URL:         "http://localhost",
		Capabilities: a2a.Capabilities{
			Streaming:              true,
			StateTransitionHistory: true,
		},
		Skills: []a2a.Skill{
			{ID: "chat", Name: "Chat", Description: "General chat"},
			{ID: "code", Name: "Code Review", Description: "Review code"},
		},
	}
}

func newTestServer(t *testing.T) (*httptest.Server, *a2a.Server) {
	t.Helper()

	agent := testutil.MockAgent("test-agent", "Hello from A2A!")
	sessSvc := session.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:           "a2a-test",
		Agent:             agent,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		t.Fatalf("runner.New: %v", err)
	}

	srv := a2a.NewServer(r, testCard())
	ts := httptest.NewServer(srv)
	return ts, srv
}

func TestAgentCard(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatalf("GET agent-card: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if card.Name != "test-agent" {
		t.Errorf("expected name test-agent, got %q", card.Name)
	}
	if len(card.Skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(card.Skills))
	}
	if !card.Capabilities.Streaming {
		t.Error("expected streaming capability")
	}
}

func TestAgentCardViaClient(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	client := a2a.NewClient(ts.URL)
	card, err := client.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("GetAgentCard: %v", err)
	}

	if card.Name != "test-agent" {
		t.Errorf("expected name test-agent, got %q", card.Name)
	}
}

func TestSendTask(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	client := a2a.NewClient(ts.URL)
	task, err := client.SendTask(context.Background(), a2a.SendTaskRequest{
		Message: a2a.Message{
			Role:  "user",
			Parts: []a2a.Part{a2a.TextPart("Hello!")},
		},
	})
	if err != nil {
		t.Fatalf("SendTask: %v", err)
	}

	if task.ID == "" {
		t.Error("expected task ID")
	}
	if task.State != a2a.TaskStateSubmitted {
		t.Errorf("expected submitted state, got %q", task.State)
	}
}

func TestSendAndWait(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	client := a2a.NewClient(ts.URL)
	task, err := client.SendTask(context.Background(), a2a.SendTaskRequest{
		Message: a2a.Message{
			Role:  "user",
			Parts: []a2a.Part{a2a.TextPart("Hello!")},
		},
	})
	if err != nil {
		t.Fatalf("SendTask: %v", err)
	}

	// Wait for completion
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	completed, err := client.WaitForCompletion(ctx, task.ID, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForCompletion: %v", err)
	}

	if completed.State != a2a.TaskStateCompleted {
		t.Errorf("expected completed state, got %q", completed.State)
	}

	// Should have agent response messages
	if len(completed.Messages) < 2 {
		t.Errorf("expected at least 2 messages (user + agent), got %d", len(completed.Messages))
	}
}

func TestGetTask(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	client := a2a.NewClient(ts.URL)
	task, _ := client.SendTask(context.Background(), a2a.SendTaskRequest{
		Message: a2a.Message{
			Role:  "user",
			Parts: []a2a.Part{a2a.TextPart("test")},
		},
	})

	fetched, err := client.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if fetched.ID != task.ID {
		t.Errorf("expected ID %s, got %s", task.ID, fetched.ID)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	client := a2a.NewClient(ts.URL)
	_, err := client.GetTask(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestCancelTask(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	client := a2a.NewClient(ts.URL)
	task, _ := client.SendTask(context.Background(), a2a.SendTaskRequest{
		Message: a2a.Message{
			Role:  "user",
			Parts: []a2a.Part{a2a.TextPart("test")},
		},
	})

	// Small delay to let it start
	time.Sleep(10 * time.Millisecond)

	canceled, err := client.CancelTask(context.Background(), task.ID)
	if err != nil {
		// May already be completed if agent is fast
		return
	}
	if canceled.State != a2a.TaskStateCanceled {
		t.Errorf("expected canceled, got %q", canceled.State)
	}
}

func TestCancelNotFound(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	client := a2a.NewClient(ts.URL)
	_, err := client.CancelTask(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTaskStateIsTerminal(t *testing.T) {
	tests := []struct {
		state    a2a.TaskState
		terminal bool
	}{
		{a2a.TaskStateSubmitted, false},
		{a2a.TaskStateWorking, false},
		{a2a.TaskStateInputRequired, false},
		{a2a.TaskStateCompleted, true},
		{a2a.TaskStateFailed, true},
		{a2a.TaskStateCanceled, true},
		{a2a.TaskStateRejected, true},
	}

	for _, tt := range tests {
		if got := tt.state.IsTerminal(); got != tt.terminal {
			t.Errorf("state %q: expected terminal=%v, got %v", tt.state, tt.terminal, got)
		}
	}
}

func TestAgentCardJSON(t *testing.T) {
	card := testCard()
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	if raw["protocol"] != "a2a/1.0" {
		t.Errorf("expected protocol a2a/1.0, got %v", raw["protocol"])
	}
}

func TestTextPart(t *testing.T) {
	p := a2a.TextPart("hello")
	if p.Type != "text" || p.Text != "hello" {
		t.Errorf("unexpected part: %+v", p)
	}
}

func TestDataPart(t *testing.T) {
	p := a2a.DataPart(map[string]int{"x": 1})
	if p.Type != "data" {
		t.Errorf("expected data type, got %q", p.Type)
	}
}

func TestTaskHistory(t *testing.T) {
	ts, _ := newTestServer(t)
	defer ts.Close()

	client := a2a.NewClient(ts.URL)
	task, _ := client.SendTask(context.Background(), a2a.SendTaskRequest{
		Message: a2a.Message{
			Role:  "user",
			Parts: []a2a.Part{a2a.TextPart("test")},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	completed, err := client.WaitForCompletion(ctx, task.ID, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForCompletion: %v", err)
	}

	// History should show: submitted → working → completed
	if len(completed.History) < 3 {
		t.Errorf("expected at least 3 history entries, got %d", len(completed.History))
	}
}
