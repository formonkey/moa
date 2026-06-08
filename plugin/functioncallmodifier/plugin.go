// Package functioncallmodifier provides a plugin that dynamically modifies
// function call declarations and responses. It can add extra parameters to
// tool schemas and extract those parameters from function call responses
// into session state.
package functioncallmodifier

import (
	"fmt"
	"maps"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
)

// Config for the function call modifier.
type Config struct {
	// Predicate decides which tools to modify. Return true to modify.
	Predicate func(toolName string) bool
	// Args are extra parameters to inject into matching tool declarations.
	Args map[string]*genai.Schema
	// OverrideDescription optionally modifies the tool description.
	OverrideDescription func(originalDescription string) string
}

// New creates a function call modifier plugin.
func New(cfg Config) *plugin.Plugin {
	return &plugin.Plugin{
		Name: "function-call-modifier",
		BeforeModelCallback: func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			// Modify tool declarations before sending to the model
			for _, t := range req.Tools {
				if t.FunctionDeclarations == nil {
					continue
				}
				for _, decl := range t.FunctionDeclarations {
					if !cfg.Predicate(decl.Name) {
						continue
					}

					if decl.Parameters == nil {
						decl.Parameters = &genai.Schema{
							Type:       "OBJECT",
							Properties: map[string]*genai.Schema{},
						}
					}
					if decl.Parameters.Properties == nil {
						decl.Parameters.Properties = map[string]*genai.Schema{}
					}

					maps.Copy(decl.Parameters.Properties, cfg.Args)

					if cfg.OverrideDescription != nil {
						decl.Description = cfg.OverrideDescription(decl.Description)
					}
				}
			}
			return nil, nil
		},
		AfterModelCallback: func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
			// Extract injected args from function calls and save to state
			if resp == nil || resp.Content == nil {
				return nil, nil
			}

			for _, part := range resp.Content.Parts {
				if fc := part.FunctionCall; fc != nil {
					if !cfg.Predicate(fc.Name) {
						continue
					}
					for name := range cfg.Args {
						arg, hasArg := fc.Args[name]
						if !hasArg {
							continue
						}
						// Remove the injected arg from the function call
						delete(fc.Args, name)
						// Save to state
						stateKey := fmt.Sprintf("%s/%s", fc.ID, name)
						ctx.State().Set(stateKey, arg)
					}
				}
			}
			return nil, nil
		},
	}
}
