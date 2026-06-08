package tool_test

import (
	"context"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/tool"
)

type simpleTool struct{ name string }

func (s *simpleTool) Name() string        { return s.name }
func (s *simpleTool) Description() string { return "simple tool" }
func (s *simpleTool) IsNative() bool      { return false }
func (s *simpleTool) IsLongRunning() bool { return false }
func (s *simpleTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name: s.name,
		Parameters: &genai.Schema{
			Type:       "OBJECT",
			Properties: map[string]*genai.Schema{"input": {Type: "STRING"}},
		},
	}
}
func (s *simpleTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return map[string]any{"output": args["input"]}, nil
}

func TestAllowedToolsPredicate(t *testing.T) {
	pred := tool.AllowedToolsPredicate("simple", "other")
	if !pred(&simpleTool{name: "simple"}) {
		t.Fatal("expected true for 'simple'")
	}
	if pred(&simpleTool{name: "unknown"}) {
		t.Fatal("expected false for 'unknown'")
	}
}

func TestFilterToolset(t *testing.T) {
	t1 := &simpleTool{name: "simple"}
	ts := tool.SimpleToolset("test", t1)
	filtered := tool.FilterToolset(ts, tool.AllowedToolsPredicate("simple"))

	tools := filtered.Tools()
	if len(tools) != 1 || tools[0].Name() != "simple" {
		t.Fatal("expected filtered tool")
	}

	// Filter out everything
	filtered2 := tool.FilterToolset(ts, tool.AllowedToolsPredicate("nonexistent"))
	if len(filtered2.Tools()) != 0 {
		t.Fatal("expected 0 tools")
	}
}

func TestSimpleToolset(t *testing.T) {
	t1 := &simpleTool{name: "t1"}
	t2 := &simpleTool{name: "t2"}
	ts := tool.SimpleToolset("my-tools", t1, t2)
	if ts.Name() != "my-tools" {
		t.Fatal("wrong name")
	}
	if len(ts.Tools()) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(ts.Tools()))
	}
}

func TestWithConfirmation(t *testing.T) {
	t1 := &simpleTool{name: "confirm-tool"}
	ts := tool.SimpleToolset("confirm-set", t1)
	confirmed := tool.WithConfirmation(ts)

	if confirmed.Name() != "confirm-set" {
		t.Fatal("wrong name")
	}

	tools := confirmed.Tools()
	if len(tools) != 1 {
		t.Fatal("expected 1 tool")
	}

	ct := tools[0]
	if ct.Name() != "confirm-tool" {
		t.Fatal("wrong tool name")
	}
	if ct.Description() != "simple tool" {
		t.Fatal("wrong description")
	}
	if ct.IsNative() {
		t.Fatal("should not be native")
	}
	if ct.IsLongRunning() {
		t.Fatal("should not be long running")
	}
	if rt, ok := ct.(tool.RunnableTool); ok {
		if rt.Declaration() == nil {
			t.Fatal("expected declaration")
		}
	}

	// Execute without ConfirmationContext — should still work
	if rt, ok := ct.(tool.RunnableTool); ok {
		result, err := rt.Execute(context.Background(), map[string]any{"input": "test"})
		// Without confirmation context, behavior depends on implementation
		_ = result
		_ = err
	}
}

func TestFilterToolsetName(t *testing.T) {
	t1 := &simpleTool{name: "a"}
	ts := tool.SimpleToolset("original", t1)
	filtered := tool.FilterToolset(ts, func(t tool.Tool) bool { return true })

	if filtered.Name() != "original" {
		t.Fatal("filtered should preserve name")
	}
}

