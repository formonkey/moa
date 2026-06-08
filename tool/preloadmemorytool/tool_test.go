package preloadmemorytool_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/memory"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool/preloadmemorytool"
)

type mockMemSearcher struct{}

func (m *mockMemSearcher) SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	return &memory.SearchResponse{
		Memories: []memory.Entry{
			{Author: "user", Content: &genai.Content{Parts: []*genai.Part{{Text: "remembered thing"}}}, Timestamp: time.Now()},
		},
	}, nil
}

func TestPreloadMemoryAccessors(t *testing.T) {
	pt := preloadmemorytool.New(&mockMemSearcher{}, "app", "user1")
	if pt.Name() != "preload_memory" {
		t.Fatalf("wrong name: %s", pt.Name())
	}
	if pt.Description() == "" {
		t.Fatal("expected description")
	}
	if pt.IsNative() {
		t.Fatal("not native")
	}
	if pt.IsLongRunning() {
		t.Fatal("not long running")
	}
}

func TestPreloadMemoryProcessRequest(t *testing.T) {
	pt := preloadmemorytool.New(&mockMemSearcher{}, "app", "user1")
	userContent := &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "hello"}}}
	req := &model.LLMRequest{}

	err := pt.ProcessRequest(context.Background(), userContent, req)
	if err != nil {
		t.Fatalf("ProcessRequest: %v", err)
	}
	// Should have injected memory into system instruction
	if req.SystemInstruction == nil {
		t.Fatal("expected system instruction with memories")
	}
}
