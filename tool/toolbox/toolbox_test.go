package toolbox

import (
	"context"
	"testing"

	"github.com/formonkey/moa/tool/functiontool"
)

// mockTool creates a simple no-op tool for testing.
func mockTool(name, desc string) *mockToolImpl {
	t, _ := functiontool.New(functiontool.Config{
		Name:        name,
		Description: desc,
	}, func(_ context.Context, args struct{}) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})
	return &mockToolImpl{inner: t}
}

type mockToolImpl struct {
	inner interface {
		Name() string
	}
}

func (m *mockToolImpl) Name() string        { return m.inner.Name() }
func (m *mockToolImpl) Description() string { return "" }
func (m *mockToolImpl) IsNative() bool      { return false }
func (m *mockToolImpl) IsLongRunning() bool { return false }

func TestNewToolbox(t *testing.T) {
	tb := New()
	if tb == nil {
		t.Fatal("expected non-nil toolbox")
	}
	if tb.Name() != "toolbox" {
		t.Errorf("expected name 'toolbox', got %q", tb.Name())
	}
}

func TestToolsInitiallyOnlyMeta(t *testing.T) {
	tb := New()
	tb.Register("file_ops", "File operations", mockTool("read_file", ""), mockTool("write_file", ""))
	tb.Register("code_intel", "Code intelligence", mockTool("codegraph_search", ""))

	tools := tb.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (discover_tools only), got %d", len(tools))
	}
	if tools[0].Name() != "discover_tools" {
		t.Errorf("expected 'discover_tools', got %q", tools[0].Name())
	}
}

func TestActivateCategory(t *testing.T) {
	tb := New()
	tb.Register("file_ops", "File operations", mockTool("read_file", ""), mockTool("write_file", ""))
	tb.Register("code_intel", "Code intelligence", mockTool("codegraph_search", ""))

	// Activate file_ops
	ok := tb.ActivateCategory("file_ops")
	if !ok {
		t.Fatal("expected ActivateCategory to return true")
	}

	tools := tb.Tools()
	// discover_tools + read_file + write_file
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools after activation, got %d", len(tools))
	}

	// Check that codegraph_search is NOT active
	for _, tool := range tools {
		if tool.Name() == "codegraph_search" {
			t.Error("codegraph_search should not be active")
		}
	}
}

func TestActivateAll(t *testing.T) {
	tb := New()
	tb.Register("file_ops", "File operations", mockTool("read_file", ""), mockTool("write_file", ""))
	tb.Register("code_intel", "Code intelligence", mockTool("codegraph_search", ""))

	tb.ActivateAll()

	tools := tb.Tools()
	// discover_tools + 3 registered
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools after ActivateAll, got %d", len(tools))
	}
}

func TestDiscoverToolsList(t *testing.T) {
	tb := New()
	tb.Register("file_ops", "File operations", mockTool("read_file", ""), mockTool("write_file", ""))
	tb.Register("shell", "Shell commands", mockTool("run_command", ""))

	result, err := tb.handleDiscover(context.Background(), discoverArgs{Category: "list"})
	if err != nil {
		t.Fatalf("handleDiscover failed: %v", err)
	}
	if result.Status != "categories" {
		t.Errorf("expected status 'categories', got %q", result.Status)
	}
	if len(result.Categories) != 2 {
		t.Errorf("expected 2 categories, got %d", len(result.Categories))
	}
}

func TestDiscoverToolsActivate(t *testing.T) {
	tb := New()
	tb.Register("file_ops", "File operations", mockTool("read_file", ""), mockTool("write_file", ""))

	result, err := tb.handleDiscover(context.Background(), discoverArgs{Category: "file_ops"})
	if err != nil {
		t.Fatalf("handleDiscover failed: %v", err)
	}
	if result.Status != "activated" {
		t.Errorf("expected status 'activated', got %q", result.Status)
	}
	if len(result.Activated) != 2 {
		t.Errorf("expected 2 activated tools, got %d", len(result.Activated))
	}

	// Tools should now be available
	tools := tb.Tools()
	if len(tools) != 3 { // discover_tools + 2 activated
		t.Errorf("expected 3 tools after discover, got %d", len(tools))
	}
}

func TestDiscoverToolsFuzzyMatch(t *testing.T) {
	tb := New()
	tb.Register("file_operations", "Read write and edit files", mockTool("read_file", ""))
	tb.Register("code_intelligence", "Code graph search", mockTool("codegraph_search", ""))

	// Fuzzy match by description
	result, err := tb.handleDiscover(context.Background(), discoverArgs{Category: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "activated" {
		t.Errorf("expected fuzzy match to activate, got status %q", result.Status)
	}
}

func TestDiscoverToolsNotFound(t *testing.T) {
	tb := New()
	tb.Register("file_ops", "File operations", mockTool("read_file", ""))

	result, err := tb.handleDiscover(context.Background(), discoverArgs{Category: "quantum_computing"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "not_found" {
		t.Errorf("expected status 'not_found', got %q", result.Status)
	}
}

func TestActivateCategoryNonExistent(t *testing.T) {
	tb := New()
	ok := tb.ActivateCategory("nonexistent")
	if ok {
		t.Error("expected false for non-existent category")
	}
}

func TestConcurrentAccess(t *testing.T) {
	tb := New()
	tb.Register("cat1", "Category 1", mockTool("tool1", ""))
	tb.Register("cat2", "Category 2", mockTool("tool2", ""))

	done := make(chan bool, 10)
	for i := 0; i < 5; i++ {
		go func() {
			tb.ActivateCategory("cat1")
			_ = tb.Tools()
			done <- true
		}()
		go func() {
			tb.ActivateCategory("cat2")
			_ = tb.Tools()
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
