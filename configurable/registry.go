// Package configurable provides a YAML/JSON declarative configuration system
// for building agent trees, tools, and callbacks from config files.
//
// This is the go-brain equivalent of ADK's internal/configurable package.
// It enables visual builders and no-code agent orchestration.
package configurable

import (
	"context"
	"fmt"
	"sync"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/tool"
)

// AgentFactory builds an agent from raw config bytes and the config file path.
type AgentFactory func(ctx context.Context, configBytes []byte, configPath string) (agent.Agent, error)

// ToolFactory builds a tool from optional args.
type ToolFactory func(ctx context.Context, args map[string]any) (tool.Tool, error)

// ToolsetFactory builds a toolset from optional args.
type ToolsetFactory func(ctx context.Context, args map[string]any) (tool.Toolset, error)

// CallbackFactory is any registered callback (typed at resolve time).
type CallbackFactory = any

// PluginFactory builds a plugin from optional args.
type PluginFactory func(ctx context.Context, args map[string]any) (*plugin.Plugin, error)

var (
	registryMu       sync.RWMutex
	agentFactories   = make(map[string]AgentFactory)
	toolFactories    = make(map[string]any) // ToolFactory or ToolsetFactory
	callbackRegistry = make(map[string]any)
	pluginFactories  = make(map[string]PluginFactory)
	agentCache       = make(map[string]agent.Agent) // keyed by absolute path
)

// RegisterAgent registers an agent factory by agent_class name.
// Returns error if a factory with the same name is already registered.
func RegisterAgent(name string, factory AgentFactory) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := agentFactories[name]; dup {
		return fmt.Errorf("configurable: RegisterAgent called twice for %q", name)
	}
	agentFactories[name] = factory
	return nil
}

// RegisterToolFactory registers a tool factory by tool name.
func RegisterToolFactory(name string, factory ToolFactory) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := toolFactories[name]; dup {
		return fmt.Errorf("configurable: RegisterToolFactory called twice for %q", name)
	}
	toolFactories[name] = factory
	return nil
}

// RegisterToolsetFactory registers a toolset factory by name.
func RegisterToolsetFactory(name string, factory ToolsetFactory) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := toolFactories[name]; dup {
		return fmt.Errorf("configurable: RegisterToolsetFactory called twice for %q", name)
	}
	toolFactories[name] = factory
	return nil
}

// RegisterCallback registers a named callback for YAML reference.
func RegisterCallback(name string, callback any) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := callbackRegistry[name]; dup {
		return fmt.Errorf("configurable: RegisterCallback called twice for %q", name)
	}
	callbackRegistry[name] = callback
	return nil
}

// RegisterPluginFactory registers a plugin factory by plugin name.
//
// Example:
//
//	configurable.RegisterPluginFactory("cost-governor", func(ctx context.Context, args map[string]any) (*plugin.Plugin, error) {
//	    return costgovernor.New(costgovernor.Config{...}), nil
//	})
func RegisterPluginFactory(name string, factory PluginFactory) error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := pluginFactories[name]; dup {
		return fmt.Errorf("configurable: RegisterPluginFactory called twice for %q", name)
	}
	pluginFactories[name] = factory
	return nil
}

// ResolveToolReference looks up a tool or toolset by name from the registry.
func ResolveToolReference(ctx context.Context, toolName string, args map[string]any) (tool.Tool, tool.Toolset, error) {
	if toolName == "" {
		return nil, nil, fmt.Errorf("configurable: tool name cannot be empty")
	}

	registryMu.RLock()
	factory, ok := toolFactories[toolName]
	registryMu.RUnlock()

	if !ok {
		return nil, nil, fmt.Errorf("configurable: tool %q not found in registry", toolName)
	}

	if tf, ok := factory.(ToolFactory); ok {
		t, err := tf(ctx, args)
		return t, nil, err
	}
	if tsf, ok := factory.(ToolsetFactory); ok {
		ts, err := tsf(ctx, args)
		return nil, ts, err
	}
	return nil, nil, fmt.Errorf("configurable: tool %q has unknown factory type %T", toolName, factory)
}

// ResolveCallbackReference looks up a named callback from the registry.
func ResolveCallbackReference(_ context.Context, callbackName string) (any, error) {
	if callbackName == "" {
		return nil, fmt.Errorf("configurable: callback name cannot be empty")
	}

	registryMu.RLock()
	cb, ok := callbackRegistry[callbackName]
	registryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("configurable: callback %q not found in registry", callbackName)
	}
	return cb, nil
}

// ResolvePluginReference looks up a plugin factory by name and builds it.
func ResolvePluginReference(ctx context.Context, pluginName string, args map[string]any) (*plugin.Plugin, error) {
	if pluginName == "" {
		return nil, fmt.Errorf("configurable: plugin name cannot be empty")
	}

	registryMu.RLock()
	factory, ok := pluginFactories[pluginName]
	registryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("configurable: plugin %q not found in registry", pluginName)
	}
	return factory(ctx, args)
}

// cacheAgent stores a resolved agent by its absolute path.
func cacheAgent(absPath string, a agent.Agent) {
	registryMu.Lock()
	defer registryMu.Unlock()
	agentCache[absPath] = a
}

// getCachedAgent retrieves a previously resolved agent by path.
func getCachedAgent(absPath string) (agent.Agent, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	a, ok := agentCache[absPath]
	return a, ok
}

// getAgentFactory retrieves an agent factory by agent_class name.
func getAgentFactory(agentClass string) (AgentFactory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := agentFactories[agentClass]
	return f, ok
}
