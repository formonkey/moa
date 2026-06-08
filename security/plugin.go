package security

import (
	"fmt"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/tool"
)

// NewPlugin creates a plugin that enforces a security policy on all tool calls.
// Attach it to the runner to sandbox agent execution.
//
// Usage:
//
//	policy := security.Standard("/path/to/project")
//	plug := security.NewPlugin(policy)
//	runner, _ := runner.New(runner.Config{Plugins: []*plugin.Plugin{plug}})
func NewPlugin(policy Policy) *plugin.Plugin {
	return &plugin.Plugin{
		Name: "security",
		BeforeToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			// Enforce the policy
			if err := policy.Enforce(t.Name(), args); err != nil {
				return nil, fmt.Errorf("[security] blocked: %w", err)
			}
			// Return nil to proceed with original args
			return nil, nil
		},
	}
}
