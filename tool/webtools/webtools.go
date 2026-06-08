// Package webtools provides web research tools for MOA agents.
//
// Includes web search (via DuckDuckGo, no API key needed) and URL fetching
// with HTML-to-text conversion. Designed for researcher, marketing, and
// content-creation agents.
//
// Usage:
//
//	webtools.Register()
//	// Tools are now available in swarm.yaml: web_search, fetch_url
package webtools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/functiontool"
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
}

// Register creates all web tools and registers them in the configurable registry.
func Register() error {
	tools, err := NewToolset()
	if err != nil {
		return fmt.Errorf("webtools: %w", err)
	}

	for _, t := range tools {
		toolRef := t
		_ = configurable.RegisterToolFactory(t.Name(), func(ctx context.Context, args map[string]any) (tool.Tool, error) {
			return toolRef, nil
		})
	}
	return nil
}

// NewToolset creates all web tools.
func NewToolset() ([]tool.Tool, error) {
	ws, err := newWebSearch()
	if err != nil {
		return nil, err
	}
	fu, err := newFetchURL()
	if err != nil {
		return nil, err
	}
	return []tool.Tool{ws, fu}, nil
}

// --- web_search ---

type webSearchArgs struct {
	Query      string `json:"query" jsonschema:"description=Search query (e.g. 'Angular 21 signal forms'),required"`
	MaxResults int    `json:"max_results" jsonschema:"description=Maximum number of results to return (default 5)"`
}

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type webSearchResult struct {
	Results []SearchResult `json:"results"`
	Query   string         `json:"query"`
	Count   int            `json:"count"`
}

func newWebSearch() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "web_search",
		Description: "Search the web for information. Returns titles, URLs, and snippets. Use fetch_url to read full page content.",
	}, func(ctx context.Context, args webSearchArgs) (webSearchResult, error) {
		maxResults := args.MaxResults
		if maxResults <= 0 {
			maxResults = 5
		}
		if maxResults > 10 {
			maxResults = 10
		}

		results, err := searchDuckDuckGo(ctx, args.Query, maxResults)
		if err != nil {
			return webSearchResult{}, fmt.Errorf("web_search: %w", err)
		}

		return webSearchResult{
			Results: results,
			Query:   args.Query,
			Count:   len(results),
		}, nil
	})
}

// searchDuckDuckGo queries DuckDuckGo's HTML lite endpoint and parses results.
func searchDuckDuckGo(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MOA-Bot/1.0)")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d from DuckDuckGo", resp.StatusCode)
	}

	return parseDDGResults(resp.Body, maxResults)
}

// parseDDGResults extracts search results from DuckDuckGo's HTML lite page.
func parseDDGResults(body io.Reader, maxResults int) ([]SearchResult, error) {
	doc, err := html.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	var results []SearchResult
	var current SearchResult
	var inResultLink bool
	var inSnippet bool

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if len(results) >= maxResults {
			return
		}

		if n.Type == html.ElementNode {
			classes := getAttr(n, "class")

			// Result link: <a class="result__a">
			if n.Data == "a" && strings.Contains(classes, "result__a") {
				inResultLink = true
				current.URL = getAttr(n, "href")
				// Clean DDG redirect URL
				if strings.Contains(current.URL, "uddg=") {
					if u, err := url.Parse(current.URL); err == nil {
						if decoded := u.Query().Get("uddg"); decoded != "" {
							current.URL = decoded
						}
					}
				}
			}

			// Snippet: <a class="result__snippet">
			if n.Data == "a" && strings.Contains(classes, "result__snippet") {
				inSnippet = true
			}
		}

		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if inResultLink {
					current.Title += text
				}
				if inSnippet {
					current.Snippet += text + " "
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		// Close tags
		if n.Type == html.ElementNode {
			if n.Data == "a" && inResultLink && strings.Contains(getAttr(n, "class"), "result__a") {
				inResultLink = false
			}
			if n.Data == "a" && inSnippet && strings.Contains(getAttr(n, "class"), "result__snippet") {
				inSnippet = false
				current.Snippet = strings.TrimSpace(current.Snippet)
				if current.Title != "" && current.URL != "" {
					results = append(results, current)
				}
				current = SearchResult{}
			}
		}
	}

	walk(doc)
	return results, nil
}

// --- fetch_url ---

type fetchURLArgs struct {
	URL       string `json:"url" jsonschema:"description=URL to fetch (e.g. https://angular.dev/guide/signals),required"`
	MaxLength int    `json:"max_length" jsonschema:"description=Maximum text length to return in characters (default 4000)"`
}
type fetchURLResult struct {
	Content string `json:"content"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	Length  int    `json:"length"`
}

func newFetchURL() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "fetch_url",
		Description: "Fetch a web page and return its text content (HTML stripped). Use web_search first to find relevant URLs.",
	}, func(ctx context.Context, args fetchURLArgs) (fetchURLResult, error) {
		maxLen := args.MaxLength
		if maxLen <= 0 {
			maxLen = 4000
		}
		if maxLen > 16000 {
			maxLen = 16000
		}

		req, err := http.NewRequestWithContext(ctx, "GET", args.URL, nil)
		if err != nil {
			return fetchURLResult{}, fmt.Errorf("invalid URL: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MOA-Bot/1.0)")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain")

		resp, err := httpClient.Do(req)
		if err != nil {
			return fetchURLResult{}, fmt.Errorf("fetch failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return fetchURLResult{}, fmt.Errorf("HTTP %d from %s", resp.StatusCode, args.URL)
		}

		// Read limited body
		limited := io.LimitReader(resp.Body, 500_000) // 500KB max raw HTML
		body, err := io.ReadAll(limited)
		if err != nil {
			return fetchURLResult{}, fmt.Errorf("read error: %w", err)
		}

		contentType := resp.Header.Get("Content-Type")
		var text, title string

		if strings.Contains(contentType, "text/html") || strings.Contains(contentType, "xhtml") {
			text, title = htmlToText(string(body))
		} else {
			text = string(body)
		}

		// Trim to max length
		if len(text) > maxLen {
			text = text[:maxLen] + "\n...[truncated]..."
		}

		return fetchURLResult{
			Content: text,
			URL:     args.URL,
			Title:   title,
			Length:  len(text),
		}, nil
	})
}

// htmlToText converts HTML to plain text, extracting meaningful content.
func htmlToText(rawHTML string) (string, string) {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML, ""
	}

	var title string
	var textBuilder strings.Builder
	var inTitle, inScript, inStyle bool

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				inTitle = true
			case "script", "noscript":
				inScript = true
			case "style":
				inStyle = true
			case "br":
				textBuilder.WriteString("\n")
			case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr", "blockquote", "article", "section":
				textBuilder.WriteString("\n")
			}
		}

		if n.Type == html.TextNode && !inScript && !inStyle {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				if inTitle {
					title = text
				}
				textBuilder.WriteString(text + " ")
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				inTitle = false
			case "script", "noscript":
				inScript = false
			case "style":
				inStyle = false
			case "p", "h1", "h2", "h3", "h4", "h5", "h6":
				textBuilder.WriteString("\n")
			}
		}
	}

	walk(doc)

	// Clean up: collapse multiple newlines
	text := textBuilder.String()
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(text), title
}

// --- helpers ---

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
