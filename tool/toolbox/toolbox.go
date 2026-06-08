// Package toolbox provides a lazy tool loading system for moa agents.
//
// Instead of registering all tools upfront (consuming context window),
// agents start with a single `discover_tools` meta-tool. When the agent
// needs specific capabilities, it calls discover_tools to activate them.
// Activated tools appear in the next model call automatically.
//
// This pattern dramatically reduces context usage:
//   - Without toolbox: 12 tools × ~170 tokens = ~2,000 tokens always
//   - With toolbox: 1 tool + only activated = ~400-600 tokens
//
// Usage:
//
//	tb := toolbox.New()
//	tb.Register("file_ops", "File operations", readFile, writeFile, editFile)
//	tb.Register("code_intel", "Code intelligence", codegraphSearch, codegraphExplore)
//
//	agent, _ := llmagent.New(llmagent.Config{
//	    Toolsets: []tool.Toolset{tb},
//	})
package toolbox

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/functiontool"
)

// Category groups related tools under a name and description.
type Category struct {
	Name        string
	Description string
	Tools       []tool.Tool
}

// Toolbox is a lazy-loading tool registry.
// It exposes a single `discover_tools` meta-tool initially.
// When the agent calls discover_tools, the requested tools are activated
// and appear in subsequent model calls.
type Toolbox struct {
	mu         sync.RWMutex
	categories map[string]*Category
	active     map[string]bool // tool name → activated
	metaTool   tool.Tool
}

var _ tool.Toolset = (*Toolbox)(nil)

// New creates a new Toolbox.
func New() *Toolbox {
	tb := &Toolbox{
		categories: make(map[string]*Category),
		active:     make(map[string]bool),
	}

	// Create the meta-tool
	meta, _ := functiontool.New(functiontool.Config{
		Name: "discover_tools",
		Description: "Discover and activate tools for your task. " +
			"Call this to see available tool categories and activate the ones you need. " +
			"Activated tools become available for use in your next action.",
	}, tb.handleDiscover)
	tb.metaTool = meta

	return tb
}

// Register adds a category of tools to the toolbox.
func (tb *Toolbox) Register(name, description string, tools ...tool.Tool) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.categories[name] = &Category{
		Name:        name,
		Description: description,
		Tools:       tools,
	}
}

// RegisterCategory adds a pre-built category.
func (tb *Toolbox) RegisterCategory(cat Category) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.categories[cat.Name] = &cat
}

// ActivateAll activates all tools immediately (bypass lazy loading).
func (tb *Toolbox) ActivateAll() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	for _, cat := range tb.categories {
		for _, t := range cat.Tools {
			tb.active[t.Name()] = true
		}
	}
}

// ActivateCategory activates all tools in a category.
func (tb *Toolbox) ActivateCategory(category string) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	cat, ok := tb.categories[category]
	if !ok {
		return false
	}
	for _, t := range cat.Tools {
		tb.active[t.Name()] = true
	}
	return true
}

// Name returns the toolset name.
func (tb *Toolbox) Name() string { return "toolbox" }

// Tools returns the discover_tools meta-tool plus all currently activated tools.
// This is called by llmagent.collectTools() on each iteration.
func (tb *Toolbox) Tools() []tool.Tool {
	tb.mu.RLock()
	defer tb.mu.RUnlock()

	// Always include the meta-tool
	result := []tool.Tool{tb.metaTool}

	// Add activated tools
	for _, cat := range tb.categories {
		for _, t := range cat.Tools {
			if tb.active[t.Name()] {
				result = append(result, t)
			}
		}
	}

	return result
}

// --- discover_tools handler ---

type discoverArgs struct {
	Category string `json:"category" jsonschema:"description=Category name to activate. Use 'list' to see all categories"`
}

type discoverResult struct {
	Status     string   `json:"status"`
	Categories []string `json:"categories,omitempty"`
	Activated  []string `json:"activated,omitempty"`
	Message    string   `json:"message"`
}

func (tb *Toolbox) handleDiscover(_ context.Context, args discoverArgs) (discoverResult, error) {
	query := strings.TrimSpace(strings.ToLower(args.Category))

	// List all categories
	if query == "" || query == "list" || query == "help" {
		return tb.listCategories(), nil
	}

	// Activate by exact category name
	if tb.ActivateCategory(query) {
		return tb.activatedResult(query), nil
	}

	// Fuzzy match: try to find categories matching the query
	tb.mu.RLock()
	var matched []string
	for name, cat := range tb.categories {
		if strings.Contains(strings.ToLower(name), query) ||
			strings.Contains(strings.ToLower(cat.Description), query) {
			matched = append(matched, name)
		}
	}
	tb.mu.RUnlock()

	if len(matched) == 0 {
		return discoverResult{
			Status:  "not_found",
			Message: fmt.Sprintf("No category matching %q. Use 'list' to see available categories.", query),
		}, nil
	}

	// Activate all matched categories
	for _, name := range matched {
		tb.ActivateCategory(name)
	}

	var activatedTools []string
	tb.mu.RLock()
	for _, name := range matched {
		if cat, ok := tb.categories[name]; ok {
			for _, t := range cat.Tools {
				activatedTools = append(activatedTools, t.Name())
			}
		}
	}
	tb.mu.RUnlock()

	return discoverResult{
		Status:    "activated",
		Activated: activatedTools,
		Message:   fmt.Sprintf("Activated %d tools from categories: %s. These tools are now available.", len(activatedTools), strings.Join(matched, ", ")),
	}, nil
}

func (tb *Toolbox) listCategories() discoverResult {
	tb.mu.RLock()
	defer tb.mu.RUnlock()

	var lines []string
	for name, cat := range tb.categories {
		var toolNames []string
		for _, t := range cat.Tools {
			toolNames = append(toolNames, t.Name())
		}
		lines = append(lines, fmt.Sprintf("- %s: %s [tools: %s]", name, cat.Description, strings.Join(toolNames, ", ")))
	}

	return discoverResult{
		Status:     "categories",
		Categories: lines,
		Message:    "Available categories. Call discover_tools with a category name to activate its tools.",
	}
}

func (tb *Toolbox) activatedResult(category string) discoverResult {
	tb.mu.RLock()
	defer tb.mu.RUnlock()

	cat := tb.categories[category]
	var toolNames []string
	for _, t := range cat.Tools {
		toolNames = append(toolNames, t.Name())
	}

	return discoverResult{
		Status:    "activated",
		Activated: toolNames,
		Message:   fmt.Sprintf("Activated %d tools from '%s'. These tools are now available for use.", len(toolNames), category),
	}
}
