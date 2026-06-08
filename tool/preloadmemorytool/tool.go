// Package preloadmemorytool provides a tool that automatically preloads
// relevant memory into the system instructions for each LLM request.
//
// Unlike loadmemorytool which is called explicitly by the model, this tool
// runs automatically and injects relevant past conversations as context.
package preloadmemorytool

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/memory"
	"github.com/formonkey/moa/model"
)

const preloadTemplate = `The following content is from your previous conversations with the user.
They may be useful for answering the user's current query.
<PAST_CONVERSATIONS>
%s
</PAST_CONVERSATIONS>`

// MemorySearcher defines the minimal interface for memory search.
type MemorySearcher interface {
	SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error)
}

type preloadMemoryTool struct {
	searcher MemorySearcher
	appName  string
	userID   string
}

// New creates a new preload_memory tool.
func New(searcher MemorySearcher, appName, userID string) *preloadMemoryTool {
	return &preloadMemoryTool{
		searcher: searcher,
		appName:  appName,
		userID:   userID,
	}
}

func (t *preloadMemoryTool) Name() string        { return "preload_memory" }
func (t *preloadMemoryTool) Description() string { return "Preloads relevant memory for the current user." }
func (t *preloadMemoryTool) IsNative() bool      { return false }
func (t *preloadMemoryTool) IsLongRunning() bool { return false }

// ProcessRequest searches memory using the user's current query and injects
// relevant past conversations into system instructions automatically.
func (t *preloadMemoryTool) ProcessRequest(ctx context.Context, userContent *genai.Content, req *model.LLMRequest) error {
	if userContent == nil || len(userContent.Parts) == 0 || userContent.Parts[0].Text == "" {
		return nil
	}
	userQuery := userContent.Parts[0].Text

	resp, err := t.searcher.SearchMemory(ctx, &memory.SearchRequest{
		AppName: t.appName,
		UserID:  t.userID,
		Query:   userQuery,
	})
	if err != nil {
		return fmt.Errorf("preload memory search failed: %v", err)
	}

	if resp == nil || len(resp.Memories) == 0 {
		return nil
	}

	memoryText := formatMemories(resp.Memories)
	if memoryText == "" {
		return nil
	}

	instruction := fmt.Sprintf(preloadTemplate, memoryText)
	appendInstruction(req, instruction)
	return nil
}

func formatMemories(memories []memory.Entry) string {
	var lines []string
	for _, mem := range memories {
		text := extractText(mem)
		if text == "" {
			continue
		}
		if !mem.Timestamp.IsZero() {
			lines = append(lines, fmt.Sprintf("Time: %s", mem.Timestamp.Format(time.RFC3339)))
		}
		if mem.Author != "" {
			text = fmt.Sprintf("%s: %s", mem.Author, text)
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func extractText(mem memory.Entry) string {
	if mem.Content == nil || len(mem.Content.Parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range mem.Content.Parts {
		if part.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(part.Text)
	}
	return b.String()
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
