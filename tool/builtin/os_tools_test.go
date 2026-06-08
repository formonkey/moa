package builtin_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/formonkey/moa/tool/builtin"
)

func TestExecuteCommandTool(t *testing.T) {
	ct := &builtin.ExecuteCommandTool{}
	if ct.Name() != "execute_command" {
		t.Fatalf("wrong name: %s", ct.Name())
	}
	if ct.Description() == "" {
		t.Fatal("empty description")
	}
	if ct.IsNative() {
		t.Fatal("should not be native")
	}
	_ = ct.IsLongRunning()
	if ct.Declaration() == nil {
		t.Fatal("nil declaration")
	}

	result, err := ct.Execute(context.Background(), map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
}

func TestExecuteCommandToolMissingArg(t *testing.T) {
	ct := &builtin.ExecuteCommandTool{}
	_, err := ct.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestWriteFileTool(t *testing.T) {
	dir := t.TempDir()
	wf := &builtin.WriteFileTool{WorkspaceDir: dir}
	if wf.Name() != "write_file" {
		t.Fatal("wrong name")
	}
	if wf.Description() == "" {
		t.Fatal("empty description")
	}
	if wf.IsNative() {
		t.Fatal("should not be native")
	}
	if wf.IsLongRunning() {
		t.Fatal("should not be long running")
	}
	if wf.Declaration() == nil {
		t.Fatal("nil declaration")
	}

	// Execute a write
	result, err := wf.Execute(context.Background(), map[string]any{
		"path":    "subdir/test.txt",
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	// Verify file was created
	data, err := os.ReadFile(filepath.Join(dir, "subdir", "test.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty file")
	}
}

func TestWriteFileToolPathTraversal(t *testing.T) {
	dir := t.TempDir()
	wf := &builtin.WriteFileTool{WorkspaceDir: dir}

	_, err := wf.Execute(context.Background(), map[string]any{
		"path":    "../../etc/passwd",
		"content": "bad",
	})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestWriteFileToolAbsPath(t *testing.T) {
	dir := t.TempDir()
	wf := &builtin.WriteFileTool{WorkspaceDir: dir}

	_, err := wf.Execute(context.Background(), map[string]any{
		"path":    "/etc/passwd",
		"content": "bad",
	})
	if err == nil {
		t.Fatal("expected error for absolute path")
	}
}

func TestWriteFileToolMissingArgs(t *testing.T) {
	wf := &builtin.WriteFileTool{WorkspaceDir: t.TempDir()}
	_, err := wf.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing args")
	}
}
