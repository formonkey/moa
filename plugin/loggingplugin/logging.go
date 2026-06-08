// Package loggingplugin provides a plugin that logs agent lifecycle events.
package loggingplugin

import (
	"log"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

// New creates a logging plugin that logs all lifecycle events.
func New() *plugin.Plugin {
	return &plugin.Plugin{
		Name: "logging",
		BeforeRunCallback: func(ctx agent.CallbackContext) error {
			log.Printf("[go-brain] ▶ Run started for agent %q", ctx.AgentName())
			return nil
		},
		AfterRunCallback: func(ctx agent.CallbackContext) error {
			log.Printf("[go-brain] ◼ Run completed for agent %q", ctx.AgentName())
			return nil
		},
		BeforeAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			log.Printf("[go-brain] → Agent %q starting", ctx.AgentName())
			return nil, nil
		},
		AfterAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			log.Printf("[go-brain] ← Agent %q finished", ctx.AgentName())
			return nil, nil
		},
		BeforeModelCallback: func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			toolCount := 0
			for _, t := range req.Tools {
				if t.FunctionDeclarations != nil {
					toolCount += len(t.FunctionDeclarations)
				}
			}
			log.Printf("[go-brain] 🤖 Model call for %q (model=%q, tools=%d, contents=%d)",
				ctx.AgentName(), req.Model, toolCount, len(req.Contents))
			return nil, nil
		},
		AfterModelCallback: func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
			if resp != nil && resp.Content != nil {
				partCount := len(resp.Content.Parts)
				log.Printf("[go-brain] ✅ Model response for %q (parts=%d, finish=%s)",
					ctx.AgentName(), partCount, resp.FinishReason)
			}
			return nil, nil
		},
		OnModelErrorCallback: func(ctx agent.CallbackContext, req *model.LLMRequest, err error) (*model.LLMResponse, error) {
			log.Printf("[go-brain] ❌ Model error for %q: %v", ctx.AgentName(), err)
			return nil, nil // Don't handle, let other plugins or default handle it
		},
		BeforeToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			log.Printf("[go-brain] 🔧 Tool %q called by %q", t.Name(), ctx.AgentName())
			return nil, nil
		},
		AfterToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error) {
			log.Printf("[go-brain] ✔ Tool %q completed for %q", t.Name(), ctx.AgentName())
			return nil, nil
		},
		OnToolErrorCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
			log.Printf("[go-brain] ❌ Tool %q error for %q: %v", t.Name(), ctx.AgentName(), err)
			return nil, nil
		},
	}
}
