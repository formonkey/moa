// Package scraper provides a Go-native web scraping tool for moa agents.
//
// It fetches a URL via HTTP, parses the HTML, extracts the main content
// using a simple readability heuristic, and returns clean text or markdown.
// No external dependencies (no Playwright, no Node.js).
//
// Usage:
//
//	tool := scraper.NewScrapeTool(scraper.Config{})
//	agent, _ := llmagent.New(llmagent.Config{
//	    Tools: []tool.Tool{tool},
//	})
package scraper

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"google.golang.org/genai"

	"github.com/formonkey/moa/tool"
)

// Config configures the scraper tool.
type Config struct {
	// UserAgent for HTTP requests. Default: "Mozilla/5.0 (compatible; moa-scraper/1.0)"
	UserAgent string
	// Timeout for HTTP requests. Default: 30s.
	Timeout time.Duration
	// MaxBodySize limits the response body size in bytes. Default: 5MB.
	MaxBodySize int64
}

func (c *Config) defaults() {
	if c.UserAgent == "" {
		c.UserAgent = "Mozilla/5.0 (compatible; moa-scraper/1.0)"
	}
	if c.Timeout <= 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxBodySize <= 0 {
		c.MaxBodySize = 5 * 1024 * 1024 // 5MB
	}
}

// NewScrapeTool creates a RunnableTool that scrapes a URL and returns clean content.
func NewScrapeTool(cfg Config) tool.RunnableTool {
	cfg.defaults()
	return &scrapeTool{cfg: cfg}
}

type scrapeTool struct {
	cfg Config
}

func (t *scrapeTool) Name() string        { return "scrape_url" }
func (t *scrapeTool) Description() string {
	return "Fetch a webpage and extract its content as clean markdown or text. " +
		"Use this to read web pages, documentation, articles, blog posts, etc. " +
		"Returns the page title and main content without navigation, ads, or boilerplate."
}
func (t *scrapeTool) IsNative() bool      { return false }
func (t *scrapeTool) IsLongRunning() bool { return false }

func (t *scrapeTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"url": {
					Type:        genai.TypeString,
					Description: "The URL of the webpage to scrape",
				},
				"format": {
					Type:        genai.TypeString,
					Description: "Output format: 'markdown' (default), 'text', or 'html'",
					Enum:        []string{"markdown", "text", "html"},
				},
			},
			Required: []string{"url"},
		},
	}
}

func (t *scrapeTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	urlStr, _ := args["url"].(string)
	if urlStr == "" {
		return nil, fmt.Errorf("scrape_url: missing required argument 'url'")
	}

	// Validate URL scheme
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		urlStr = "https://" + urlStr
	}

	format, _ := args["format"].(string)
	if format == "" {
		format = "markdown"
	}

	// Fetch the page
	client := &http.Client{Timeout: t.cfg.Timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("scrape_url: invalid URL %q: %w", urlStr, err)
	}
	req.Header.Set("User-Agent", t.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrape_url: failed to fetch %q: %w", urlStr, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape_url: HTTP %d for %q", resp.StatusCode, urlStr)
	}

	// Read body with size limit
	body := io.LimitReader(resp.Body, t.cfg.MaxBodySize)

	// Parse HTML
	doc, err := html.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("scrape_url: failed to parse HTML: %w", err)
	}

	// Extract title
	title := extractTitle(doc)

	// Extract main content
	mainNode := findMainContent(doc)
	if mainNode == nil {
		mainNode = doc
	}

	var content string
	switch format {
	case "html":
		content = renderHTML(mainNode)
	case "text":
		content = renderText(mainNode)
	default: // markdown
		content = renderMarkdown(mainNode)
	}

	// Trim excessive whitespace
	content = collapseWhitespace(content)

	return map[string]any{
		"title":   title,
		"url":     urlStr,
		"content": content,
		"format":  format,
	}, nil
}

// --- HTML content extraction ---

// extractTitle finds the <title> element text.
func extractTitle(doc *html.Node) string {
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Title {
			title = textContent(n)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			if title != "" {
				return
			}
		}
	}
	walk(doc)
	return strings.TrimSpace(title)
}

// findMainContent uses a simple readability heuristic to find the main content node.
// Priority: <article> > <main> > <div role="main"> > largest text-dense <div>.
func findMainContent(doc *html.Node) *html.Node {
	// First pass: look for semantic elements
	if node := findElement(doc, atom.Article); node != nil {
		return node
	}
	if node := findElement(doc, atom.Main); node != nil {
		return node
	}
	if node := findByRole(doc, "main"); node != nil {
		return node
	}

	// Second pass: find the <div> with the most text content
	// (simple readability heuristic)
	var bestNode *html.Node
	var bestScore int

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.DataAtom == atom.Div || n.DataAtom == atom.Section) {
			score := contentScore(n)
			if score > bestScore {
				bestScore = score
				bestNode = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if bestNode != nil && bestScore > 100 {
		return bestNode
	}

	// Fallback: return <body>
	return findElement(doc, atom.Body)
}

// contentScore estimates how "content-rich" a node is.
// Counts text length, penalizes navigation/script elements.
func contentScore(n *html.Node) int {
	if n == nil {
		return 0
	}
	score := 0
	var walk func(*html.Node)
	walk = func(child *html.Node) {
		if child.Type == html.TextNode {
			text := strings.TrimSpace(child.Data)
			score += len(text)
		}
		if child.Type == html.ElementNode {
			// Penalize navigation and boilerplate
			switch child.DataAtom {
			case atom.Nav, atom.Header, atom.Footer, atom.Script, atom.Style, atom.Noscript, atom.Iframe:
				return // skip entirely
			case atom.A:
				// Links contribute less than text paragraphs
				score -= len(textContent(child)) / 2
			case atom.P:
				// Paragraphs are strong signals
				score += 10
			}
		}
		for c := child.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return score
}

// --- Rendering ---

// renderMarkdown converts an HTML node tree to markdown.
func renderMarkdown(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	renderMarkdownNode(&sb, n, 0)
	return sb.String()
}

func renderMarkdownNode(sb *strings.Builder, n *html.Node, depth int) {
	if n == nil {
		return
	}

	switch n.Type {
	case html.TextNode:
		text := n.Data
		if strings.TrimSpace(text) != "" {
			sb.WriteString(text)
		}
		return

	case html.ElementNode:
		// Skip non-content elements
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Iframe, atom.Nav,
			atom.Header, atom.Footer, atom.Svg, atom.Form, atom.Button:
			return
		}

		switch n.DataAtom {
		case atom.H1:
			sb.WriteString("\n# ")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("\n\n")
			return
		case atom.H2:
			sb.WriteString("\n## ")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("\n\n")
			return
		case atom.H3:
			sb.WriteString("\n### ")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("\n\n")
			return
		case atom.H4:
			sb.WriteString("\n#### ")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("\n\n")
			return
		case atom.H5:
			sb.WriteString("\n##### ")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("\n\n")
			return
		case atom.H6:
			sb.WriteString("\n###### ")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("\n\n")
			return
		case atom.P:
			sb.WriteString("\n")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("\n")
			return
		case atom.Br:
			sb.WriteString("\n")
			return
		case atom.Hr:
			sb.WriteString("\n---\n")
			return
		case atom.Strong, atom.B:
			sb.WriteString("**")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("**")
			return
		case atom.Em, atom.I:
			sb.WriteString("*")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("*")
			return
		case atom.Code:
			sb.WriteString("`")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("`")
			return
		case atom.Pre:
			sb.WriteString("\n```\n")
			renderMarkdownChildren(sb, n, depth)
			sb.WriteString("\n```\n")
			return
		case atom.A:
			href := getAttr(n, "href")
			if href != "" {
				sb.WriteString("[")
				renderMarkdownChildren(sb, n, depth)
				sb.WriteString("](")
				sb.WriteString(href)
				sb.WriteString(")")
			} else {
				renderMarkdownChildren(sb, n, depth)
			}
			return
		case atom.Img:
			alt := getAttr(n, "alt")
			src := getAttr(n, "src")
			if src != "" {
				sb.WriteString("![")
				sb.WriteString(alt)
				sb.WriteString("](")
				sb.WriteString(src)
				sb.WriteString(")")
			}
			return
		case atom.Ul:
			sb.WriteString("\n")
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.DataAtom == atom.Li {
					sb.WriteString("- ")
					renderMarkdownChildren(sb, c, depth+1)
					sb.WriteString("\n")
				}
			}
			return
		case atom.Ol:
			sb.WriteString("\n")
			i := 1
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && c.DataAtom == atom.Li {
					sb.WriteString(fmt.Sprintf("%d. ", i))
					renderMarkdownChildren(sb, c, depth+1)
					sb.WriteString("\n")
					i++
				}
			}
			return
		case atom.Blockquote:
			sb.WriteString("\n> ")
			text := textContent(n)
			sb.WriteString(strings.ReplaceAll(strings.TrimSpace(text), "\n", "\n> "))
			sb.WriteString("\n")
			return
		case atom.Table:
			renderMarkdownTable(sb, n)
			return
		}
	}

	// Default: recurse into children
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderMarkdownNode(sb, c, depth)
	}
}

func renderMarkdownChildren(sb *strings.Builder, n *html.Node, depth int) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderMarkdownNode(sb, c, depth)
	}
}

// renderMarkdownTable converts an HTML table to a markdown table.
func renderMarkdownTable(sb *strings.Builder, n *html.Node) {
	var rows [][]string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.DataAtom == atom.Tr {
			var cells []string
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (c.DataAtom == atom.Td || c.DataAtom == atom.Th) {
					cells = append(cells, strings.TrimSpace(textContent(c)))
				}
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)

	if len(rows) == 0 {
		return
	}

	sb.WriteString("\n")
	// Header row
	sb.WriteString("| " + strings.Join(rows[0], " | ") + " |\n")
	// Separator
	sep := make([]string, len(rows[0]))
	for i := range sep {
		sep[i] = "---"
	}
	sb.WriteString("| " + strings.Join(sep, " | ") + " |\n")
	// Data rows
	for _, row := range rows[1:] {
		// Pad if needed
		for len(row) < len(rows[0]) {
			row = append(row, "")
		}
		sb.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	sb.WriteString("\n")
}

// renderText extracts plain text from HTML, stripping all tags.
func renderText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	renderTextNode(&sb, n)
	return sb.String()
}

func renderTextNode(sb *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		text := strings.TrimSpace(n.Data)
		if text != "" {
			sb.WriteString(text)
			sb.WriteString(" ")
		}
	case html.ElementNode:
		switch n.DataAtom {
		case atom.Script, atom.Style, atom.Noscript, atom.Iframe, atom.Svg:
			return
		case atom.P, atom.Br, atom.Hr, atom.Div, atom.Li,
			atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
			sb.WriteString("\n")
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		renderTextNode(sb, c)
	}
}

// renderHTML serializes an HTML node back to an HTML string.
func renderHTML(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	html.Render(&sb, n)
	return sb.String()
}

// --- Utility functions ---

// textContent returns the concatenated text content of a node and its children.
func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

// findElement finds the first element with the given atom.
func findElement(n *html.Node, a atom.Atom) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, a); found != nil {
			return found
		}
	}
	return nil
}

// findByRole finds the first element with role="main".
func findByRole(n *html.Node, role string) *html.Node {
	if n == nil {
		return nil
	}
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			if attr.Key == "role" && attr.Val == role {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findByRole(c, role); found != nil {
			return found
		}
	}
	return nil
}

// getAttr returns the value of an attribute on a node.
func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// collapseWhitespace collapses runs of 3+ newlines into 2 and trims the result.
func collapseWhitespace(s string) string {
	// Collapse runs of blank lines
	lines := strings.Split(s, "\n")
	var result []string
	blankCount := 0
	for _, line := range lines {
		trimmed := strings.TrimRightFunc(line, unicode.IsSpace)
		if trimmed == "" {
			blankCount++
			if blankCount <= 2 {
				result = append(result, "")
			}
		} else {
			blankCount = 0
			result = append(result, trimmed)
		}
	}
	return strings.TrimSpace(strings.Join(result, "\n"))
}

var _ tool.RunnableTool = (*scrapeTool)(nil)
