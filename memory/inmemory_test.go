package memory_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/memory"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
)

func TestInMemoryServiceAddAndSearch(t *testing.T) {
	svc := memory.InMemoryService()
	ctx := context.Background()

	// Create a session with events
	sessSvc := session.InMemoryService()
	createResp, _ := sessSvc.Create(ctx, &session.CreateRequest{
		AppName:   "app",
		UserID:    "user1",
		SessionID: "sess1",
	})
	sess := createResp.Session

	// Add an event
	evt := session.NewEvent("inv-1")
	evt.Author = "agent"
	evt.LLMResponse = model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: []*genai.Part{{Text: "The weather in Madrid is sunny today"}},
		},
	}
	evt.Timestamp = time.Now()
	sessSvc.AppendEvent(ctx, sess, evt)

	// Re-fetch session to get events
	getResp, _ := sessSvc.Get(ctx, &session.GetRequest{AppName: "app", UserID: "user1", SessionID: "sess1"})

	// Index into memory
	err := svc.AddSessionToMemory(ctx, getResp.Session)
	if err != nil {
		t.Fatalf("AddSessionToMemory failed: %v", err)
	}

	// Search — should find by keyword
	resp, err := svc.SearchMemory(ctx, &memory.SearchRequest{
		AppName: "app",
		UserID:  "user1",
		Query:   "weather Madrid",
	})
	if err != nil {
		t.Fatalf("SearchMemory failed: %v", err)
	}
	if len(resp.Memories) == 0 {
		t.Fatal("expected at least 1 memory match")
	}

	// Search — no match
	resp2, _ := svc.SearchMemory(ctx, &memory.SearchRequest{
		AppName: "app",
		UserID:  "user1",
		Query:   "quantum entanglement",
	})
	if len(resp2.Memories) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(resp2.Memories))
	}
}

func TestSearchEmptyMemory(t *testing.T) {
	svc := memory.InMemoryService()
	ctx := context.Background()

	resp, err := svc.SearchMemory(ctx, &memory.SearchRequest{
		AppName: "app",
		UserID:  "unknown",
		Query:   "anything",
	})
	if err != nil {
		t.Fatalf("SearchMemory failed: %v", err)
	}
	if len(resp.Memories) != 0 {
		t.Fatal("expected empty result")
	}
}
