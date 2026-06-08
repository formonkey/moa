package webtools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- htmlToText ---

func TestHtmlToText(t *testing.T) {
	html := `<html><head><title>Test Page</title></head>
<body>
<h1>Hello World</h1>
<p>This is a <strong>test</strong> paragraph.</p>
<script>var x = 1;</script>
<style>.foo { color: red; }</style>
<p>Second paragraph.</p>
</body></html>`

	text, title := htmlToText(html)

	if title != "Test Page" {
		t.Errorf("expected title 'Test Page', got %q", title)
	}
	if !strings.Contains(text, "Hello World") {
		t.Error("expected 'Hello World' in text")
	}
	if !strings.Contains(text, "test") {
		t.Error("expected 'test' in text")
	}
	if strings.Contains(text, "var x = 1") {
		t.Error("script content should be stripped")
	}
	if strings.Contains(text, "color: red") {
		t.Error("style content should be stripped")
	}
}

func TestHtmlToTextPlain(t *testing.T) {
	text, title := htmlToText("just plain text")
	if title != "" {
		t.Errorf("expected empty title for plain text, got %q", title)
	}
	if !strings.Contains(text, "just plain text") {
		t.Error("expected plain text preserved")
	}
}

// --- parseDDGResults ---

func TestParseDDGResults(t *testing.T) {
	// Simulated DuckDuckGo HTML lite results
	ddgHTML := `<html><body>
<div class="results">
  <div class="result">
    <a class="result__a" href="https://example.com/page1">First Result Title</a>
    <a class="result__snippet">This is the first snippet text.</a>
  </div>
  <div class="result">
    <a class="result__a" href="https://example.com/page2">Second Result</a>
    <a class="result__snippet">Another snippet here.</a>
  </div>
  <div class="result">
    <a class="result__a" href="https://example.com/page3">Third Result</a>
    <a class="result__snippet">Third snippet content.</a>
  </div>
</div>
</body></html>`

	results, err := parseDDGResults(strings.NewReader(ddgHTML), 5)
	if err != nil {
		t.Fatalf("parseDDGResults failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].Title != "First Result Title" {
		t.Errorf("expected 'First Result Title', got %q", results[0].Title)
	}
	if results[0].URL != "https://example.com/page1" {
		t.Errorf("expected URL page1, got %q", results[0].URL)
	}
	if !strings.Contains(results[0].Snippet, "first snippet") {
		t.Errorf("expected snippet content, got %q", results[0].Snippet)
	}
}

func TestParseDDGResultsMaxResults(t *testing.T) {
	ddgHTML := `<html><body>
<a class="result__a" href="https://a.com">A</a><a class="result__snippet">A desc</a>
<a class="result__a" href="https://b.com">B</a><a class="result__snippet">B desc</a>
<a class="result__a" href="https://c.com">C</a><a class="result__snippet">C desc</a>
</body></html>`

	results, _ := parseDDGResults(strings.NewReader(ddgHTML), 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

// --- fetch_url (with local server) ---

func TestFetchURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Test</title></head><body>
<h1>Angular Signals</h1>
<p>Signals are reactive primitives in Angular 21.</p>
</body></html>`))
	}))
	defer server.Close()

	tool, _ := newFetchURL()
	rt := tool.(interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	})

	result, err := rt.Execute(context.Background(), map[string]any{
		"url": server.URL,
	})
	if err != nil {
		t.Fatalf("fetch_url failed: %v", err)
	}

	content, _ := result["content"].(string)
	if !strings.Contains(content, "Angular Signals") {
		t.Errorf("expected 'Angular Signals' in content, got: %s", content)
	}

	title, _ := result["title"].(string)
	if title != "Test" {
		t.Errorf("expected title 'Test', got %q", title)
	}
}

func TestFetchURLPlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("plain text content"))
	}))
	defer server.Close()

	tool, _ := newFetchURL()
	rt := tool.(interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	})

	result, err := rt.Execute(context.Background(), map[string]any{
		"url": server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	content, _ := result["content"].(string)
	if content != "plain text content" {
		t.Errorf("expected plain text, got: %s", content)
	}
}

func TestFetchURLMaxLength(t *testing.T) {
	longText := strings.Repeat("x", 10000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(longText))
	}))
	defer server.Close()

	tool, _ := newFetchURL()
	rt := tool.(interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	})

	result, err := rt.Execute(context.Background(), map[string]any{
		"url":        server.URL,
		"max_length": float64(100),
	})
	if err != nil {
		t.Fatal(err)
	}

	content, _ := result["content"].(string)
	if len(content) > 200 { // 100 + truncation message
		t.Errorf("content should be truncated, got length %d", len(content))
	}
}

func TestFetchURL404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer server.Close()

	tool, _ := newFetchURL()
	rt := tool.(interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	})

	_, err := rt.Execute(context.Background(), map[string]any{
		"url": server.URL,
	})
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// --- web_search (with mock server) ---

func TestWebSearchWithMock(t *testing.T) {
	// Create a mock DDG server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>
<a class="result__a" href="https://angular.dev">Angular Dev</a>
<a class="result__snippet">Official Angular documentation.</a>
<a class="result__a" href="https://blog.angular.dev">Angular Blog</a>
<a class="result__snippet">Latest Angular news and updates.</a>
</body></html>`))
	}))
	defer server.Close()

	// We can't easily mock the DDG URL in the tool, so test parseDDGResults directly
	results, err := parseDDGResults(strings.NewReader(`<html><body>
<a class="result__a" href="https://angular.dev">Angular Dev</a>
<a class="result__snippet">Official docs.</a>
</body></html>`), 5)

	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].Title != "Angular Dev" {
		t.Errorf("unexpected title: %q", results[0].Title)
	}
}

// --- NewToolset ---

func TestNewToolset(t *testing.T) {
	tools, err := NewToolset()
	if err != nil {
		t.Fatalf("NewToolset failed: %v", err)
	}

	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name()] = true
	}

	if !names["web_search"] {
		t.Error("missing web_search tool")
	}
	if !names["fetch_url"] {
		t.Error("missing fetch_url tool")
	}
}

// --- Register ---

func TestRegister(t *testing.T) {
	if err := Register(); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
}
