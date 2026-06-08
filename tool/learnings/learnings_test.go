package learnings

import (
	"context"
	"path/filepath"
	"testing"
)

func setupStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "learnings"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSaveAndSearch(t *testing.T) {
	store := setupStore(t)

	_, err := store.Save("naming", "Use FEATURE_ prefix for feature constants", "Login feature", "user_feedback")
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.Search("naming", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Category != "naming" {
		t.Errorf("expected naming, got %s", results[0].Category)
	}
}

func TestSearchByLesson(t *testing.T) {
	store := setupStore(t)
	store.Save("style", "Always use private readonly for inject()", "Auth service", "user_feedback")
	store.Save("architecture", "Never inject services directly in components", "Dashboard", "user_feedback")

	results, err := store.Search("inject", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for 'inject', got %d", len(results))
	}
}

func TestSearchNoResults(t *testing.T) {
	store := setupStore(t)
	store.Save("naming", "Use I prefix for interfaces", "", "user_feedback")

	results, err := store.Search("nonexistent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestAll(t *testing.T) {
	store := setupStore(t)
	store.Save("a", "lesson A", "", "user_feedback")
	store.Save("b", "lesson B", "", "user_feedback")
	store.Save("c", "lesson C", "", "user_feedback")

	all, err := store.All(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3, got %d", len(all))
	}
}

func TestAllLimit(t *testing.T) {
	store := setupStore(t)
	for i := 0; i < 20; i++ {
		store.Save("cat", "lesson", "", "user_feedback")
	}

	all, err := store.All(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) > 5 {
		t.Errorf("expected max 5, got %d", len(all))
	}
}

// --- Tool tests ---

type runnableTool interface {
	Execute(ctx context.Context, args map[string]any) (map[string]any, error)
}

func TestToolSaveLearning(t *testing.T) {
	store := setupStore(t)
	tools, _ := NewToolset(store)
	var saveTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "save_learning" {
			saveTool = tl.(runnableTool)
		}
	}

	result, err := saveTool.Execute(context.Background(), map[string]any{
		"category": "naming",
		"lesson":   "Use I prefix for interfaces",
		"context":  "Creating User model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "saved" {
		t.Errorf("expected saved, got %v", result["status"])
	}
}

func TestToolSearchLearnings(t *testing.T) {
	store := setupStore(t)
	store.Save("naming", "Use I prefix for interfaces", "", "user_feedback")
	store.Save("style", "readonly for inject()", "", "user_feedback")

	tools, _ := NewToolset(store)
	var searchTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "search_learnings" {
			searchTool = tl.(runnableTool)
		}
	}

	result, err := searchTool.Execute(context.Background(), map[string]any{
		"query": "naming",
	})
	if err != nil {
		t.Fatal(err)
	}
	count, _ := result["count"].(float64)
	if count != 1 {
		t.Errorf("expected 1 match, got %v", result["count"])
	}
}

func TestNewToolsetCount(t *testing.T) {
	store := setupStore(t)
	tools, err := NewToolset(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}
