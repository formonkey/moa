package developer

import (
	"context"
	"fmt"

	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/tool"
)

// Register registers the developer skill toolset factory in the configurable
// registry for YAML resolution.
//
// Call this once at startup to enable YAML-based developer agent configuration:
//
//	developer.Register()
//
// YAML example:
//
//	tools:
//	  - name: developer
//	    args:
//	      project_dir: "./"
//	      language: "go"
//	      custom_instructions: "Always use table-driven tests"
func Register() error {
	return configurable.RegisterToolsetFactory("developer", func(ctx context.Context, args map[string]any) (tool.Toolset, error) {
		cfg := Config{}
		if v, ok := args["project_dir"].(string); ok {
			cfg.ProjectDir = v
		}
		if cfg.ProjectDir == "" {
			cfg.ProjectDir = "."
		}
		if v, ok := args["language"].(string); ok {
			cfg.Language = v
		}
		if v, ok := args["framework"].(string); ok {
			cfg.Framework = v
		}
		if v, ok := args["custom_instructions"].(string); ok {
			cfg.CustomInstructions = v
		}

		skill, err := New(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("developer: %w", err)
		}
		return skill, nil
	})
}
