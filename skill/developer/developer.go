package developer

import (
	"context"
	"fmt"
	"strings"

	"github.com/formonkey/moa/skill"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/codegraph"
	"github.com/formonkey/moa/tool/devtools"
	"github.com/formonkey/moa/tool/scraper"
	"github.com/formonkey/moa/tool/toolbox"
)

// Config for the Developer Skill.
type Config struct {
	// ProjectDir is the root of the project. Required.
	ProjectDir string
	// Language hint (e.g., "go", "typescript"). If empty, auto-detected.
	Language string
	// Framework hint (e.g., "next", "gin", "angular"). If empty, auto-detected.
	Framework string
	// IncludeCodegraph enables codegraph integration. Default: true if .codegraph/ exists.
	// Set to a non-nil false to force-disable even if .codegraph/ exists.
	IncludeCodegraph *bool
	// IncludeScraper enables the scrape_url tool. Default: true.
	IncludeScraper *bool
	// CustomInstructions are appended to the system prompt.
	CustomInstructions string
	// GraphSummary is a pre-generated code graph summary to inject.
	// If empty and codegraph is loaded, a summary will be generated automatically.
	GraphSummary string
	// LazyTools enables the toolbox pattern: tools start hidden behind a
	// discover_tools meta-tool. The agent activates categories on demand,
	// reducing context usage by ~70%. Default: false.
	LazyTools bool
}

// Skill is the Developer Skill implementation.
// It combines devtools (file I/O, shell, code editing) with codegraph
// (code intelligence) into a single skill bundle.
type Skill struct {
	projectInfo   *ProjectInfo
	tools         []tool.Tool
	toolbox       *toolbox.Toolbox // non-nil when LazyTools is enabled
	directive     string
	codegraphTS   *codegraph.Toolset
}

var _ skill.Skill = (*Skill)(nil)

func (s *Skill) Name() string           { return "developer" }
func (s *Skill) Description() string    { return "Developer skill with file operations, shell execution, and code graph awareness" }
func (s *Skill) SystemDirective() string { return s.directive }
func (s *Skill) Tools() []tool.Tool {
	// When using toolbox, delegate to it for dynamic tool resolution
	if s.toolbox != nil {
		return s.toolbox.Tools()
	}
	return s.tools
}

// ProjectInfo returns the detected project metadata.
func (s *Skill) ProjectInfo() *ProjectInfo { return s.projectInfo }

// CodegraphToolset returns the codegraph toolset if loaded, or nil.
func (s *Skill) CodegraphToolset() *codegraph.Toolset { return s.codegraphTS }

// Close shuts down the codegraph MCP server if it was started.
func (s *Skill) Close() error {
	if s.codegraphTS != nil {
		return s.codegraphTS.Close()
	}
	return nil
}

// New creates a fully configured Developer Skill.
//
// It performs the following steps:
//  1. Auto-detects the project (language, framework, structure)
//  2. Creates devtools (read_file, write_file, edit_file, list_dir, search_files, run_command)
//  3. Optionally loads codegraph for code intelligence tools
//  4. Optionally adds scrape_url for web scraping
//  5. Generates a system prompt that teaches the agent to explore the graph first
func New(ctx context.Context, cfg Config) (*Skill, error) {
	if cfg.ProjectDir == "" {
		return nil, fmt.Errorf("developer: ProjectDir is required")
	}

	// Step 1: Detect project
	info, err := Detect(cfg.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("developer: project detection failed: %w", err)
	}

	// Override with hints if provided
	if cfg.Language != "" {
		info.Language = cfg.Language
	}
	if cfg.Framework != "" {
		info.Framework = cfg.Framework
	}

	s := &Skill{
		projectInfo: info,
	}

	// Step 2: Create devtools
	devTools, err := devtools.NewToolset(cfg.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("developer: failed to create devtools: %w", err)
	}

	// Step 3: Load codegraph if available
	includeCodegraph := info.HasCodegraph
	if cfg.IncludeCodegraph != nil {
		includeCodegraph = *cfg.IncludeCodegraph
	}

	var graphSummary string
	var cgTools []tool.Tool
	if includeCodegraph {
		ts, err := codegraph.NewToolset(codegraph.Config{
			ProjectDir: cfg.ProjectDir,
		})
		if err != nil {
			graphSummary = "(codegraph not available: " + err.Error() + ")"
		} else {
			if err := ts.Load(ctx); err != nil {
				graphSummary = "(codegraph failed to load: " + err.Error() + ")"
			} else {
				s.codegraphTS = ts
				cgTools = ts.Tools()

				if cfg.GraphSummary != "" {
					graphSummary = cfg.GraphSummary
				} else {
					graphSummary = generateGraphSummary(ctx, ts, info)
				}
			}
		}
	}

	// Step 4: Scraper
	includeScraper := true
	if cfg.IncludeScraper != nil {
		includeScraper = *cfg.IncludeScraper
	}
	var scraperTools []tool.Tool
	if includeScraper {
		scraperTools = append(scraperTools, scraper.NewScrapeTool(scraper.Config{}))
	}

	// Step 5: Assemble tools — either lazy (toolbox) or direct
	if cfg.LazyTools {
		tb := toolbox.New()
		tb.Register("file_ops", "Read, write, edit files and list directories", devTools...)
		if len(cgTools) > 0 {
			tb.Register("code_intel", "Search symbols, explore call graphs, impact analysis", cgTools...)
		}
		if len(scraperTools) > 0 {
			tb.Register("web", "Scrape web pages for documentation", scraperTools...)
		}
		// search_files and run_command are in devTools, but let's check
		// if there's a run_command we want always-on
		s.tools = tb.Tools() // initially just discover_tools
		// Store the toolbox so Tools() delegates to it dynamically
		s.toolbox = tb
	} else {
		s.tools = append(s.tools, devTools...)
		s.tools = append(s.tools, cgTools...)
		s.tools = append(s.tools, scraperTools...)
	}

	// Step 6: Generate system prompt
	s.directive = buildDirective(info, graphSummary, cfg.CustomInstructions, cfg.LazyTools)

	return s, nil
}

// generateGraphSummary queries codegraph to build a top-level overview.
func generateGraphSummary(ctx context.Context, ts *codegraph.Toolset, info *ProjectInfo) string {
	// Try to get the top-level structure via codegraph explore
	// Use the first entry point if available
	target := ""
	if len(info.EntryPoints) > 0 {
		target = info.EntryPoints[0]
	}

	tools := ts.Tools()
	for _, t := range tools {
		if t.Name() == "codegraph_explore" || t.Name() == "explore" {
			if rt, ok := t.(interface {
				Execute(ctx context.Context, args map[string]any) (map[string]any, error)
			}); ok {
				args := map[string]any{}
				if target != "" {
					args["path"] = target
				}
				result, err := rt.Execute(ctx, args)
				if err == nil {
					if text, ok := result["result"].(string); ok && text != "" {
						return text
					}
					if text, ok := result["text"].(string); ok && text != "" {
						return text
					}
				}
			}
		}
	}

	return "(codegraph loaded — use codegraph_search and codegraph_explore to navigate)"
}

// buildDirective creates the system prompt from project info and graph summary.
func buildDirective(info *ProjectInfo, graphSummary, customInstructions string, lazyTools bool) string {
	var sb strings.Builder

	// Header — concise
	lang := info.Language
	if lang == "" {
		lang = "unknown"
	}
	if info.Framework != "" {
		sb.WriteString(fmt.Sprintf("Expert %s developer. %s project.", lang, info.Framework))
	} else {
		sb.WriteString(fmt.Sprintf("Expert %s developer.", lang))
	}
	if info.ModuleName != "" {
		sb.WriteString(fmt.Sprintf(" Module: %s.", info.ModuleName))
	}
	if len(info.EntryPoints) > 0 {
		sb.WriteString(fmt.Sprintf(" Entry: %s.", strings.Join(info.EntryPoints, ", ")))
	}
	sb.WriteString("\n")

	// Structure — compact
	if info.Structure != "" {
		sb.WriteString("\nStructure:\n")
		sb.WriteString(info.Structure)
		sb.WriteString("\n")
	}

	// Code graph
	if graphSummary != "" {
		sb.WriteString("\nCode graph:\n")
		sb.WriteString(graphSummary)
		sb.WriteString("\n")
	}

	// Workflow — concise with example
	if lazyTools {
		sb.WriteString("\nWorkflow: discover_tools(category) → read → edit → verify.\n")
	} else if info.HasCodegraph {
		sb.WriteString("\nWorkflow: search graph → read file → edit surgically → run tests.\n")
	} else {
		sb.WriteString("\nWorkflow: search_files → read_file → edit_file → run_command.\n")
	}

	// Rules — minimal
	sb.WriteString("\nRules:\n")
	sb.WriteString("- Surgical edits only (edit_file search/replace). Never rewrite whole files.\n")
	sb.WriteString("- Read before editing. Test after changing.\n")
	if info.HasCodegraph {
		sb.WriteString("- Check codegraph_impact before modifying public symbols.\n")
	}

	// Few-shot example
	sb.WriteString("\nExample:\n")
	if info.HasCodegraph {
		sb.WriteString(`User: "Add a health check endpoint"
1. codegraph_search({"query": "router"}) → finds router setup
2. read_file({"path": "internal/router.go"}) → reads current routes
3. edit_file({"path": "internal/router.go", "search": "// routes", "replace": "// routes\n\trouter.GET(\"/health\", healthHandler)"})
4. run_command({"command": "go test ./..."})
`)
	} else {
		sb.WriteString(`User: "Add a health check endpoint"
1. search_files({"pattern": "router"}) → finds router file
2. read_file({"path": "internal/router.go"}) → reads current code
3. edit_file({"path": "internal/router.go", "search": "// routes", "replace": "// routes\n\trouter.GET(\"/health\", healthHandler)"})
4. run_command({"command": "go test ./..."})
`)
	}

	// Custom instructions
	if customInstructions != "" {
		sb.WriteString("\n" + customInstructions + "\n")
	}

	return sb.String()
}
