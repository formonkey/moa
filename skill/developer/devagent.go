package developer

import (
	"context"
	"fmt"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/llmagent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool"
)

// AgentConfig configures a developer agent.
type AgentConfig struct {
	// ProjectDir is the root of the project to work on. Required.
	ProjectDir string
	// Model is the LLM to use. Required.
	Model model.LLM
	// Name of the agent. Default: "developer".
	Name string
	// CustomInstructions appended to system prompt.
	CustomInstructions string
	// ExtraTools are additional tools beyond the default dev set.
	ExtraTools []tool.Tool
	// ExtraToolsets are additional toolsets.
	ExtraToolsets []tool.Toolset
	// Language hint. If empty, auto-detected.
	Language string
	// Framework hint. If empty, auto-detected.
	Framework string
}

// NewAgent creates a fully configured developer agent with codegraph awareness.
//
// This is the primary entry point for creating a "batteries-included" developer
// agent. It auto-detects the project, loads the code graph, and configures
// the agent with all necessary tools and a specialized system prompt.
//
// Returns the agent, a cleanup function (closes codegraph subprocess), and an error.
//
// Usage:
//
//	agent, cleanup, err := developer.NewAgent(ctx, developer.AgentConfig{
//	    ProjectDir: "./",
//	    Model:      myLLM,
//	})
//	if err != nil { log.Fatal(err) }
//	defer cleanup()
//
//	r, _ := runner.New(runner.Config{Agent: agent, ...})
func NewAgent(ctx context.Context, cfg AgentConfig) (agent.Agent, func(), error) {
	if cfg.ProjectDir == "" {
		return nil, nil, fmt.Errorf("developer: ProjectDir is required")
	}
	if cfg.Model == nil {
		return nil, nil, fmt.Errorf("developer: Model is required")
	}
	if cfg.Name == "" {
		cfg.Name = "developer"
	}

	// Create the developer skill
	devSkill, err := New(ctx, Config{
		ProjectDir:         cfg.ProjectDir,
		Language:           cfg.Language,
		Framework:          cfg.Framework,
		CustomInstructions: cfg.CustomInstructions,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("developer: failed to create skill: %w", err)
	}

	// Collect all tools
	allTools := make([]tool.Tool, 0, len(devSkill.Tools())+len(cfg.ExtraTools))
	allTools = append(allTools, devSkill.Tools()...)
	allTools = append(allTools, cfg.ExtraTools...)

	// Collect all toolsets
	var toolsets []tool.Toolset
	toolsets = append(toolsets, cfg.ExtraToolsets...)

	// Create the LLM agent
	a, err := llmagent.New(llmagent.Config{
		Name:        cfg.Name,
		Description: devSkill.Description(),
		Model:       cfg.Model,
		Instruction: devSkill.SystemDirective(),
		Tools:       allTools,
		Toolsets:    toolsets,
	})
	if err != nil {
		devSkill.Close()
		return nil, nil, fmt.Errorf("developer: failed to create agent: %w", err)
	}

	// Cleanup function closes codegraph
	cleanup := func() {
		devSkill.Close()
	}

	return a, cleanup, nil
}
