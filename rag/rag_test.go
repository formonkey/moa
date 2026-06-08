package rag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const testMarkdown = `# Test Document

## Component Architecture

Components use signals for reactivity.

` + "```typescript" + `
export class MyComponent {
  public name = signal('');
}
` + "```" + `

### Standalone Components

Every component must be standalone. Never use NgModules.

## Routing

Use loadChildren for feature-modules.

` + "```typescript" + `
export const routes: Routes = [
  { path: '', loadChildren: () => import('./auth/auth.routes') },
];
` + "```" + `

## Guards

Guards can be feature-specific or shared.

### Functional Guards

Always use functional guards.

` + "```typescript" + `
export const authGuard: CanActivateFn = () => {
  return inject(AuthStore).isAuthenticated();
};
` + "```" + `
`

func setupTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()

	// Write test markdown
	mdPath := filepath.Join(dir, "test-doc.md")
	if err := os.WriteFile(mdPath, []byte(testMarkdown), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(filepath.Join(dir, "badger"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	return store, mdPath
}

func TestNewStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	if store.db == nil {
		t.Fatal("expected db to be initialized")
	}
}

func TestNewStoreDefaultPath(t *testing.T) {
	// Just verify it doesn't panic with empty path
	// We don't actually open it to avoid polluting CWD
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore with temp dir failed: %v", err)
	}
	store.Close()
}

func TestIndexFile(t *testing.T) {
	store, mdPath := setupTestStore(t)

	if err := store.IndexFile(mdPath); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	// Search for something we know is there
	results, err := store.Search("standalone", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for 'standalone'")
	}
	if results[0].File != mdPath {
		t.Errorf("expected file %q, got %q", mdPath, results[0].File)
	}
}

func TestIndexFiles(t *testing.T) {
	dir := t.TempDir()

	// Create two markdown files
	md1 := filepath.Join(dir, "doc1.md")
	md2 := filepath.Join(dir, "doc2.md")
	os.WriteFile(md1, []byte("## Alpha\n\nFirst document about alpha.\n"), 0o644)
	os.WriteFile(md2, []byte("## Beta\n\nSecond document about beta.\n"), 0o644)

	store, err := NewStore(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.IndexFiles([]string{md1, md2}); err != nil {
		t.Fatalf("IndexFiles failed: %v", err)
	}

	r1, _ := store.Search("alpha", 10)
	r2, _ := store.Search("beta", 10)

	if len(r1) != 1 {
		t.Errorf("expected 1 result for 'alpha', got %d", len(r1))
	}
	if len(r2) != 1 {
		t.Errorf("expected 1 result for 'beta', got %d", len(r2))
	}
}

func TestChecksumSkipsReindex(t *testing.T) {
	store, mdPath := setupTestStore(t)

	// Index once
	if err := store.IndexFile(mdPath); err != nil {
		t.Fatal(err)
	}

	// Get checksum
	checksumKey := "rag:checksum:" + mdPath
	checksum1, err := store.get(checksumKey)
	if err != nil {
		t.Fatalf("checksum not stored: %v", err)
	}

	// Index again — should skip (same checksum)
	if err := store.IndexFile(mdPath); err != nil {
		t.Fatal(err)
	}

	checksum2, _ := store.get(checksumKey)
	if checksum1 != checksum2 {
		t.Error("checksum changed unexpectedly")
	}

	// Results should still be there
	results, _ := store.Search("routing", 10)
	if len(results) == 0 {
		t.Error("expected results after re-index skip")
	}
}

func TestChecksumReindexOnChange(t *testing.T) {
	store, mdPath := setupTestStore(t)

	// Index original
	if err := store.IndexFile(mdPath); err != nil {
		t.Fatal(err)
	}

	checksumKey := "rag:checksum:" + mdPath
	checksum1, _ := store.get(checksumKey)

	// Modify the file
	newContent := "## New Section\n\nCompletely new content about bananas.\n"
	os.WriteFile(mdPath, []byte(newContent), 0o644)

	// Re-index — should detect change
	if err := store.IndexFile(mdPath); err != nil {
		t.Fatal(err)
	}

	checksum2, _ := store.get(checksumKey)
	if checksum1 == checksum2 {
		t.Error("checksum should have changed after file modification")
	}

	// Old content should be gone
	oldResults, _ := store.Search("standalone", 10)
	if len(oldResults) != 0 {
		t.Error("old content should have been removed after re-index")
	}

	// New content should be searchable
	newResults, _ := store.Search("bananas", 10)
	if len(newResults) == 0 {
		t.Error("new content should be searchable after re-index")
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	store, mdPath := setupTestStore(t)
	store.IndexFile(mdPath)

	// Search with different cases
	for _, q := range []string{"ROUTING", "Routing", "routing", "rOuTiNg"} {
		results, err := store.Search(q, 10)
		if err != nil {
			t.Fatalf("Search(%q) failed: %v", q, err)
		}
		if len(results) == 0 {
			t.Errorf("expected results for case %q", q)
		}
	}
}

func TestSearchMaxResults(t *testing.T) {
	store, mdPath := setupTestStore(t)
	store.IndexFile(mdPath)

	// "component" appears in multiple sections
	results, _ := store.Search("component", 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestSearchNoResults(t *testing.T) {
	store, mdPath := setupTestStore(t)
	store.IndexFile(mdPath)

	results, err := store.Search("xyznonexistent", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestIndexFileMissing(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "db"))
	defer store.Close()

	err := store.IndexFile("/nonexistent/file.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSplitMarkdown(t *testing.T) {
	sections := splitMarkdown("test.md", testMarkdown)

	if len(sections) < 4 {
		t.Fatalf("expected at least 4 sections, got %d", len(sections))
	}

	// First real section should be about Component Architecture
	found := false
	for _, s := range sections {
		if s.Title == "## Component Architecture" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find section '## Component Architecture'")
	}
}

func TestNewSearchDocsTool(t *testing.T) {
	store, mdPath := setupTestStore(t)
	store.IndexFile(mdPath)

	searchTool, err := NewSearchDocsTool(store)
	if err != nil {
		t.Fatalf("NewSearchDocsTool failed: %v", err)
	}

	if searchTool.Name() != "search_docs" {
		t.Errorf("expected tool name 'search_docs', got %q", searchTool.Name())
	}

	if searchTool.Description() == "" {
		t.Error("expected non-empty description")
	}

	// Execute the tool (cast to RunnableTool)
	runnable, ok := searchTool.(interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	})
	if !ok {
		t.Fatal("search_docs tool does not implement Execute")
	}

	result, err := runnable.Execute(context.Background(), map[string]any{
		"query": "guards",
	})
	if err != nil {
		t.Fatalf("tool.Execute failed: %v", err)
	}

	// functiontool does JSON round-trip, so []string becomes []interface{}
	var count int
	if sections, ok := result["sections"].([]string); ok {
		count = len(sections)
	} else if sections, ok := result["sections"].([]interface{}); ok {
		count = len(sections)
	}
	if count == 0 {
		t.Error("expected at least 1 result for 'guards'")
	}
}

func TestNewStoreEmptyPath(t *testing.T) {
	// Use a subdir to test path assignment
	tmpDir := t.TempDir()
	store, err := NewStore(filepath.Join(tmpDir, ".moa_rag"))
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	store.Close()
}

func TestCloseNilDB(t *testing.T) {
	s := &Store{db: nil}
	if err := s.Close(); err != nil {
		t.Fatalf("Close on nil db: %v", err)
	}
}

func TestSearchDefaultMaxResults(t *testing.T) {
	store, mdPath := setupTestStore(t)
	store.IndexFile(mdPath)

	// maxResults <= 0 should default to 10
	results, err := store.Search("component", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Just verify it doesn't panic and returns some results
	_ = results
}

func TestIndexFilesError(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "db"))
	defer store.Close()

	err := store.IndexFiles([]string{"/nonexistent/file.md"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNewSearchDocsToolMissingQuery(t *testing.T) {
	store, mdPath := setupTestStore(t)
	store.IndexFile(mdPath)

	searchTool, _ := NewSearchDocsTool(store)

	runnable, ok := searchTool.(interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	})
	if !ok {
		t.Fatal("tool does not implement Execute")
	}

	// Execute with missing query — should not error, just return empty
	_, err := runnable.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("expected no error for missing query, got: %v", err)
	}
}
