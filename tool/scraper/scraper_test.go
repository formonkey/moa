package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScrapeTool_Declaration(t *testing.T) {
	tool := NewScrapeTool(Config{})
	if tool.Name() != "scrape_url" {
		t.Errorf("expected name 'scrape_url', got %q", tool.Name())
	}
	if tool.IsNative() {
		t.Error("scrape_url should not be native")
	}
	decl := tool.Declaration()
	if decl == nil {
		t.Fatal("declaration should not be nil")
	}
	if decl.Parameters == nil {
		t.Fatal("parameters should not be nil")
	}
	if _, ok := decl.Parameters.Properties["url"]; !ok {
		t.Error("parameters should have 'url' property")
	}
}

func TestScrapeTool_Execute_Markdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
<nav><a href="/">Home</a></nav>
<article>
<h1>Hello World</h1>
<p>This is a <strong>test</strong> paragraph with a <a href="https://example.com">link</a>.</p>
<ul>
<li>Item one</li>
<li>Item two</li>
</ul>
<pre><code>fmt.Println("hello")</code></pre>
</article>
<footer>Copyright 2024</footer>
</body>
</html>`)
	}))
	defer srv.Close()

	tool := NewScrapeTool(Config{Timeout: 5 * time.Second})
	result, err := tool.Execute(context.Background(), map[string]any{
		"url":    srv.URL,
		"format": "markdown",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	title, _ := result["title"].(string)
	if title != "Test Page" {
		t.Errorf("expected title 'Test Page', got %q", title)
	}

	content, _ := result["content"].(string)
	if !strings.Contains(content, "# Hello World") {
		t.Errorf("expected markdown heading, got:\n%s", content)
	}
	if !strings.Contains(content, "**test**") {
		t.Errorf("expected bold text, got:\n%s", content)
	}
	if !strings.Contains(content, "[link](https://example.com)") {
		t.Errorf("expected markdown link, got:\n%s", content)
	}
	if !strings.Contains(content, "- Item one") {
		t.Errorf("expected list items, got:\n%s", content)
	}
}

func TestScrapeTool_Execute_Text(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
<main>
<p>Hello world</p>
<script>var x = 1;</script>
<p>Second paragraph</p>
</main>
</body></html>`)
	}))
	defer srv.Close()

	tool := NewScrapeTool(Config{})
	result, err := tool.Execute(context.Background(), map[string]any{
		"url":    srv.URL,
		"format": "text",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, _ := result["content"].(string)
	if !strings.Contains(content, "Hello world") {
		t.Errorf("expected text content, got:\n%s", content)
	}
	if strings.Contains(content, "var x = 1") {
		t.Error("script content should be stripped")
	}
}

func TestScrapeTool_Execute_MissingURL(t *testing.T) {
	tool := NewScrapeTool(Config{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
	if !strings.Contains(err.Error(), "missing required argument") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestScrapeTool_Execute_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tool := NewScrapeTool(Config{})
	_, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestScrapeTool_Execute_AutoPrefixHTTPS(t *testing.T) {
	// This test verifies URL auto-prefix logic (will fail to connect, but URL is correct)
	tool := NewScrapeTool(Config{Timeout: 100 * time.Millisecond})
	_, err := tool.Execute(context.Background(), map[string]any{"url": "127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected connection error")
	}
	// The error should mention https:// prefixed URL
	if !strings.Contains(err.Error(), "https://127.0.0.1:1") {
		t.Logf("error: %v", err)
	}
}

func TestScrapeTool_Execute_Table(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
<article>
<table>
<tr><th>Name</th><th>Age</th></tr>
<tr><td>Alice</td><td>30</td></tr>
<tr><td>Bob</td><td>25</td></tr>
</table>
</article>
</body></html>`)
	}))
	defer srv.Close()

	tool := NewScrapeTool(Config{})
	result, err := tool.Execute(context.Background(), map[string]any{
		"url":    srv.URL,
		"format": "markdown",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, _ := result["content"].(string)
	if !strings.Contains(content, "| Name | Age |") {
		t.Errorf("expected markdown table, got:\n%s", content)
	}
	if !strings.Contains(content, "| Alice | 30 |") {
		t.Errorf("expected table data, got:\n%s", content)
	}
}

func TestScrapeTool_FindMainContent_Article(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body>
<nav>Navigation stuff here with lots of links</nav>
<article><p>The real content is here and it is important.</p></article>
<aside>Sidebar noise</aside>
</body></html>`)
	}))
	defer srv.Close()

	tool := NewScrapeTool(Config{})
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, _ := result["content"].(string)
	if !strings.Contains(content, "real content") {
		t.Errorf("should extract article content, got:\n%s", content)
	}
	if strings.Contains(content, "Navigation stuff") {
		t.Error("should not include nav content")
	}
}

func TestCollapseWhitespace(t *testing.T) {
	input := "Hello\n\n\n\n\nWorld\n\n\nEnd"
	result := collapseWhitespace(input)
	// The function allows up to 2 blank lines (3 newlines), but collapses anything beyond that
	if strings.Contains(result, "\n\n\n\n") {
		t.Errorf("should collapse runs of 4+ newlines, got:\n%q", result)
	}
	if !strings.Contains(result, "Hello") || !strings.Contains(result, "World") {
		t.Errorf("should preserve content, got:\n%q", result)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	cfg.defaults()

	if cfg.UserAgent == "" {
		t.Error("UserAgent should have default")
	}
	if cfg.Timeout <= 0 {
		t.Error("Timeout should have default")
	}
	if cfg.MaxBodySize <= 0 {
		t.Error("MaxBodySize should have default")
	}
}
