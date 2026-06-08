// Package loggingplugin provides a plugin that logs agent lifecycle events using structured logging (slog).
package loggingplugin

import (
	"log/slog"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

// New creates a logging plugin that logs all lifecycle events with structured slog output.
func New() *plugin.Plugin {
	return &plugin.Plugin{
		Name: "logging",
		BeforeRunCallback: func(ctx agent.CallbackContext) error {
			slog.Info("run started", "agent", ctx.AgentName())
			return nil
		},
		AfterRunCallback: func(ctx agent.CallbackContext) error {
			slog.Info("run completed", "agent", ctx.AgentName())
			return nil
		},
		BeforeAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			slog.Info("agent starting", "agent", ctx.AgentName())
			return nil, nil
		},
		AfterAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			slog.Info("agent finished", "agent", ctx.AgentName())
			return nil, nil
		},
		BeforeModelCallback: func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			toolCount := 0
			for _, t := range req.Tools {
				if t.FunctionDeclarations != nil {
					toolCount += len(t.FunctionDeclarations)
				}
			}
			slog.Info("model call",
				"agent", ctx.AgentName(),
				"model", req.Model,
				"tools", toolCount,
				"contents", len(req.Contents),
			)
			return nil, nil
		},
		AfterModelCallback: func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
			if resp != nil && resp.Content != nil {
				slog.Info("model response",
					"agent", ctx.AgentName(),
					"parts", len(resp.Content.Parts),
					"finish_reason", resp.FinishReason,
				)
			}
			return nil, nil
		},
		OnModelErrorCallback: func(ctx agent.CallbackContext, req *model.LLMRequest, err error) (*model.LLMResponse, error) {
			slog.Error("model error", "agent", ctx.AgentName(), "error", err)
			return nil, nil // Don't handle, let other plugins or default handle it
		},
		BeforeToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			slog.Info("tool called", "tool", t.Name(), "agent", ctx.AgentName())
			return nil, nil
		},
		AfterToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error) {
			slog.Info("tool completed", "tool", t.Name(), "agent", ctx.AgentName())
			return nil, nil
		},
		OnToolErrorCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
			slog.Error("tool error", "tool", t.Name(), "agent", ctx.AgentName(), "error", err)
			return nil, nil
		},
	}
}
