package gittools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize git repo
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %v: %s %v", args, out, err)
		}
	}

	// Create initial commit
	f := filepath.Join(dir, "README.md")
	os.WriteFile(f, []byte("# Test\n"), 0644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()

	return dir
}

func TestGitStatus(t *testing.T) {
	dir := setupGitRepo(t)
	tools, err := NewToolset(dir)
	if err != nil {
		t.Fatal(err)
	}

	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}

	// Find git_status
	var statusTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "git_status" {
			statusTool = tl.(runnableTool)
		}
	}

	result, err := statusTool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result["clean"] != true {
		t.Errorf("expected clean repo, got %v", result)
	}
}

func TestGitStatusDirty(t *testing.T) {
	dir := setupGitRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello"), 0644)

	tools, _ := NewToolset(dir)
	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}
	var statusTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "git_status" {
			statusTool = tl.(runnableTool)
		}
	}

	result, _ := statusTool.Execute(context.Background(), map[string]any{})
	if result["clean"] == true {
		t.Error("expected dirty repo")
	}
}

func TestGitCommit(t *testing.T) {
	dir := setupGitRepo(t)
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content"), 0644)

	tools, _ := NewToolset(dir)
	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}
	toolMap := make(map[string]runnableTool)
	for _, tl := range tools {
		toolMap[tl.Name()] = tl.(runnableTool)
	}

	result, err := toolMap["git_commit"].Execute(context.Background(), map[string]any{
		"message": "add test file",
		"files":   "test.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["hash"] == nil || result["hash"] == "" {
		t.Error("expected commit hash")
	}
}

func TestGitBranch(t *testing.T) {
	dir := setupGitRepo(t)
	tools, _ := NewToolset(dir)
	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}
	toolMap := make(map[string]runnableTool)
	for _, tl := range tools {
		toolMap[tl.Name()] = tl.(runnableTool)
	}

	// Create branch
	result, err := toolMap["git_branch"].Execute(context.Background(), map[string]any{
		"action": "create",
		"name":   "feature/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["current"] != "feature/test" {
		t.Errorf("expected feature/test, got %v", result["current"])
	}

	// List branches
	result, err = toolMap["git_branch"].Execute(context.Background(), map[string]any{
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	branches, ok := result["branches"]
	if !ok {
		t.Fatal("expected branches")
	}
	// JSON round-trip converts []string to []interface{}
	branchList, ok := branches.([]interface{})
	if !ok {
		t.Fatalf("unexpected type: %T", branches)
	}
	if len(branchList) < 2 {
		t.Errorf("expected at least 2 branches, got %d", len(branchList))
	}
}

func TestGitLog(t *testing.T) {
	dir := setupGitRepo(t)
	tools, _ := NewToolset(dir)
	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}
	var logTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "git_log" {
			logTool = tl.(runnableTool)
		}
	}

	result, err := logTool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	log, _ := result["log"].(string)
	if log == "" {
		t.Error("expected log output")
	}
}

func TestGitStash(t *testing.T) {
	dir := setupGitRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("modified\n"), 0644)

	tools, _ := NewToolset(dir)
	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}
	toolMap := make(map[string]runnableTool)
	for _, tl := range tools {
		toolMap[tl.Name()] = tl.(runnableTool)
	}

	// Stash
	result, err := toolMap["git_stash"].Execute(context.Background(), map[string]any{
		"action":  "save",
		"message": "test stash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "saved" {
		t.Errorf("expected saved, got %v", result["status"])
	}

	// Verify clean
	statusResult, _ := toolMap["git_status"].Execute(context.Background(), map[string]any{})
	if statusResult["clean"] != true {
		t.Error("expected clean after stash")
	}

	// Pop
	result, err = toolMap["git_stash"].Execute(context.Background(), map[string]any{
		"action": "pop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "popped" {
		t.Errorf("expected popped, got %v", result["status"])
	}
}

func TestGitWorktreeList(t *testing.T) {
	dir := setupGitRepo(t)
	tools, _ := NewToolset(dir)
	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}
	var wtTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "git_worktree" {
			wtTool = tl.(runnableTool)
		}
	}

	result, err := wtTool.Execute(context.Background(), map[string]any{
		"action": "list",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" {
		t.Errorf("expected ok, got %v", result["status"])
	}
}

func TestGitDiff(t *testing.T) {
	dir := setupGitRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("modified content\n"), 0644)

	tools, _ := NewToolset(dir)
	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}
	var diffTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "git_diff" {
			diffTool = tl.(runnableTool)
		}
	}

	result, err := diffTool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result["empty"] == true {
		t.Error("expected diff to not be empty")
	}
}

func TestNewToolsetCount(t *testing.T) {
	dir := setupGitRepo(t)
	tools, err := NewToolset(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 7 {
		t.Errorf("expected 7 tools, got %d", len(tools))
	}

	expectedNames := map[string]bool{
		"git_status":   true,
		"git_diff":     true,
		"git_commit":   true,
		"git_branch":   true,
		"git_stash":    true,
		"git_log":      true,
		"git_worktree": true,
	}
	for _, tl := range tools {
		if !expectedNames[tl.Name()] {
			t.Errorf("unexpected tool: %s", tl.Name())
		}
	}
}
