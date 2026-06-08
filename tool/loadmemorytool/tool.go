// Package loadmemorytool provides a tool that allows the LLM to search
// session memory on demand via a function call.
package loadmemorytool

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/formonkey/moa/memory"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool"
)

const memoryInstructions = `You have memory. You can use it to answer questions. If any questions need you to look up the memory, you should call load_memory function with a query.`

// MemorySearcher defines the minimal interface for memory search.
type MemorySearcher interface {
	SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error)
}

type loadMemoryTool struct {
	searcher MemorySearcher
	appName  string
	userID   string
}

// New creates a new load_memory tool.
func New(searcher MemorySearcher, appName, userID string) tool.Tool {
	return &loadMemoryTool{
		searcher: searcher,
		appName:  appName,
		userID:   userID,
	}
}

func (t *loadMemoryTool) Name() string        { return "load_memory" }
func (t *loadMemoryTool) Description() string { return "Loads the memory for the current user." }
func (t *loadMemoryTool) IsNative() bool      { return false }
func (t *loadMemoryTool) IsLongRunning() bool { return false }

func (t *loadMemoryTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        "load_memory",
		Description: "Searches memory for relevant information based on a query.",
		Parameters: &genai.Schema{
			Type: "OBJECT",
			Properties: map[string]*genai.Schema{
				"query": {
					Type:        "STRING",
					Description: "The query to search memory for.",
				},
			},
			Required: []string{"query"},
		},
	}
}

func (t *loadMemoryTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	queryRaw, ok := args["query"]
	if !ok {
		return nil, fmt.Errorf("missing required parameter: query")
	}
	query, ok := queryRaw.(string)
	if !ok {
		return nil, fmt.Errorf("query must be a string, got: %T", queryRaw)
	}

	resp, err := t.searcher.SearchMemory(ctx, &memory.SearchRequest{
		AppName: t.appName,
		UserID:  t.userID,
		Query:   query,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search memory: %w", err)
	}

	if resp == nil || len(resp.Memories) == 0 {
		return map[string]any{"memories": []any{}}, nil
	}

	// Convert memories to serializable format
	var results []map[string]any
	for _, m := range resp.Memories {
		entry := map[string]any{
			"author": m.Author,
		}
		if m.Content != nil {
			var texts []string
			for _, p := range m.Content.Parts {
				if p.Text != "" {
					texts = append(texts, p.Text)
				}
			}
			entry["content"] = strings.Join(texts, " ")
		}
		if !m.Timestamp.IsZero() {
			entry["timestamp"] = m.Timestamp.String()
		}
		results = append(results, entry)
	}

	return map[string]any{"memories": results}, nil
}

// ProcessRequest injects memory instructions into the system prompt.
func (t *loadMemoryTool) ProcessRequest(req *model.LLMRequest) {
	appendInstruction(req, memoryInstructions)
}

func appendInstruction(req *model.LLMRequest, text string) {
	if req.SystemInstruction == nil {
		req.SystemInstruction = &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: text}},
		}
		return
	}
	existing := ""
	for _, p := range req.SystemInstruction.Parts {
		existing += p.Text
	}
	req.SystemInstruction = &genai.Content{
		Role:  "system",
		Parts: []*genai.Part{{Text: strings.TrimSpace(existing) + "\n\n" + text}},
	}
}

var _ tool.RunnableTool = (*loadMemoryTool)(nil)
