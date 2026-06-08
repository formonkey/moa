package loadmemorytool_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/memory"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool/loadmemorytool"
)

type mockSearcher struct{}

func (m *mockSearcher) SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	return &memory.SearchResponse{
		Memories: []memory.Entry{
			{
				Author: "agent1",
				Content: &genai.Content{
					Parts: []*genai.Part{{Text: "remembered data"}},
				},
				Timestamp: time.Now(),
			},
		},
	}, nil
}

func TestLoadMemoryTool(t *testing.T) {
	tl := loadmemorytool.New(&mockSearcher{}, "app", "user1")

	if tl.Name() != "load_memory" {
		t.Fatal("wrong name")
	}
	if tl.IsNative() {
		t.Fatal("should not be native")
	}

	// Use RunnableTool interface
	rt := tl.(interface {
		Declaration() *genai.FunctionDeclaration
		Execute(context.Context, map[string]any) (map[string]any, error)
	})
	if rt.Declaration() == nil {
		t.Fatal("expected declaration")
	}

	result, err := rt.Execute(context.Background(), map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result["memories"] == nil {
		t.Fatal("expected memories in result")
	}
}

func TestLoadMemoryMissingQuery(t *testing.T) {
	tl := loadmemorytool.New(&mockSearcher{}, "app", "user1")
	rt := tl.(interface {
		Execute(context.Context, map[string]any) (map[string]any, error)
	})
	_, err := rt.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestLoadMemoryProcessRequest(t *testing.T) {
	tl := loadmemorytool.New(&mockSearcher{}, "app", "user1")
	rt := tl.(interface{ ProcessRequest(*model.LLMRequest) })
	req := &model.LLMRequest{}
	rt.ProcessRequest(req)
	if req.SystemInstruction == nil {
		t.Fatal("expected system instruction injection")
	}
}
