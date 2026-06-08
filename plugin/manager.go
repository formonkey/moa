package plugin

import (
	"errors"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

// Manager orchestrates the execution of multiple plugins in order.
// Thread-safe for reads after initialization — plugins should not be added
// after the runner starts.
type Manager struct {
	plugins []*Plugin
}

// NewManager creates a plugin manager with the given plugins.
// Plugins are executed in the order provided.
func NewManager(plugins ...*Plugin) *Manager {
	return &Manager{plugins: plugins}
}

// --- Lifecycle hooks ---

func (m *Manager) OnUserMessage(ctx agent.CallbackContext, msg *session.Event) (*session.Event, error) {
	for _, p := range m.plugins {
		if p.OnUserMessageCallback != nil {
			result, err := p.OnUserMessageCallback(ctx, msg)
			if result != nil || err != nil {
				return result, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) OnEvent(ctx agent.CallbackContext, event *session.Event) (*session.Event, error) {
	for _, p := range m.plugins {
		if p.OnEventCallback != nil {
			result, err := p.OnEventCallback(ctx, event)
			if result != nil || err != nil {
				return result, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) BeforeRun(ctx agent.CallbackContext) error {
	for _, p := range m.plugins {
		if p.BeforeRunCallback != nil {
			if err := p.BeforeRunCallback(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) AfterRun(ctx agent.CallbackContext) error {
	for _, p := range m.plugins {
		if p.AfterRunCallback != nil {
			if err := p.AfterRunCallback(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) BeforeAgent(ctx agent.CallbackContext) (*session.Event, error) {
	for _, p := range m.plugins {
		if p.BeforeAgentCallback != nil {
			event, err := p.BeforeAgentCallback(ctx)
			if event != nil || err != nil {
				return event, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) AfterAgent(ctx agent.CallbackContext) (*session.Event, error) {
	for _, p := range m.plugins {
		if p.AfterAgentCallback != nil {
			event, err := p.AfterAgentCallback(ctx)
			if event != nil || err != nil {
				return event, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) BeforeModel(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
	for _, p := range m.plugins {
		if p.BeforeModelCallback != nil {
			resp, err := p.BeforeModelCallback(ctx, req)
			if resp != nil || err != nil {
				return resp, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) AfterModel(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
	for _, p := range m.plugins {
		if p.AfterModelCallback != nil {
			r, err := p.AfterModelCallback(ctx, resp)
			if r != nil || err != nil {
				return r, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) OnModelError(ctx agent.CallbackContext, req *model.LLMRequest, origErr error) (*model.LLMResponse, error) {
	for _, p := range m.plugins {
		if p.OnModelErrorCallback != nil {
			resp, err := p.OnModelErrorCallback(ctx, req, origErr)
			if resp != nil || err != nil {
				return resp, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) BeforeTool(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
	for _, p := range m.plugins {
		if p.BeforeToolCallback != nil {
			result, err := p.BeforeToolCallback(ctx, t, args)
			if result != nil || err != nil {
				return result, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) AfterTool(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error) {
	for _, p := range m.plugins {
		if p.AfterToolCallback != nil {
			r, err := p.AfterToolCallback(ctx, t, args, result)
			if r != nil || err != nil {
				return r, err
			}
		}
	}
	return nil, nil
}

func (m *Manager) OnToolError(ctx agent.CallbackContext, t tool.Tool, args map[string]any, origErr error) (map[string]any, error) {
	for _, p := range m.plugins {
		if p.OnToolErrorCallback != nil {
			result, err := p.OnToolErrorCallback(ctx, t, args, origErr)
			if result != nil || err != nil {
				return result, err
			}
		}
	}
	return nil, nil
}

// Close calls CloseFunc on all plugins that have one.
// Collects all errors from all plugins using errors.Join.
func (m *Manager) Close() error {
	var errs []error
	for _, p := range m.plugins {
		if p.CloseFunc != nil {
			if err := p.CloseFunc(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
