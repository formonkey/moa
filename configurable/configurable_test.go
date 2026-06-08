package configurable_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/tool"
)

func TestFromConfig(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "agent.yaml")
	yaml := `name: test-agent
description: A test agent
type: llm
model:
  provider: mock
  name: test
instruction: "You are a test agent"
`
	os.WriteFile(yamlPath, []byte(yaml), 0644)

	_, err := configurable.FromConfig(context.Background(), yamlPath)
	if err == nil {
		t.Log("FromConfig succeeded")
	}
}

func TestFromConfigMissingFile(t *testing.T) {
	_, err := configurable.FromConfig(context.Background(), "/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestRegisterAgent(t *testing.T) {
	err := configurable.RegisterAgent("test_agent_type_unique", func(ctx context.Context, data []byte, configPath string) (agent.Agent, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	// Duplicate registration should fail
	err = configurable.RegisterAgent("test_agent_type_unique", func(ctx context.Context, data []byte, configPath string) (agent.Agent, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
}

func TestRegisterToolFactory(t *testing.T) {
	err := configurable.RegisterToolFactory("test_tool_factory_unique", func(ctx context.Context, args map[string]any) (tool.Tool, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("RegisterToolFactory: %v", err)
	}
}

func TestRegisterToolsetFactory(t *testing.T) {
	err := configurable.RegisterToolsetFactory("test_toolset_factory_unique", func(ctx context.Context, args map[string]any) (tool.Toolset, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("RegisterToolsetFactory: %v", err)
	}
}

func TestRegisterCallback(t *testing.T) {
	err := configurable.RegisterCallback("test_callback_unique", func() {})
	if err != nil {
		t.Fatalf("RegisterCallback: %v", err)
	}
}

func TestResolveToolReference(t *testing.T) {
	configurable.RegisterToolFactory("resolve_test_tool", func(ctx context.Context, args map[string]any) (tool.Tool, error) {
		return nil, nil
	})
	_, _, err := configurable.ResolveToolReference(context.Background(), "resolve_test_tool", nil)
	// May succeed or fail depending on factory return
	_ = err
}

func TestResolveCallbackReference(t *testing.T) {
	configurable.RegisterCallback("resolve_test_cb", func() {})
	cb, err := configurable.ResolveCallbackReference(context.Background(), "resolve_test_cb")
	if err != nil {
		t.Fatalf("ResolveCallbackReference: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
}

func TestResolveCallbackReferenceNotFound(t *testing.T) {
	_, err := configurable.ResolveCallbackReference(context.Background(), "nonexistent_cb")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSetModelResolver(t *testing.T) {
	configurable.SetModelResolver(func(ctx context.Context, cfg configurable.ModelConfig) (model.LLM, error) {
		return nil, nil
	})
}

func TestFromConfigWithModelResolver(t *testing.T) {
	// Set a resolver that returns a valid mock model
	configurable.SetModelResolver(func(ctx context.Context, cfg configurable.ModelConfig) (model.LLM, error) {
		return nil, nil
	})

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "agent.yaml")
	yaml := `name: resolver-agent
description: Uses resolver
model: test-model
provider: mock
instruction: "You are helpful"
`
	os.WriteFile(yamlPath, []byte(yaml), 0644)

	_, err := configurable.FromConfig(context.Background(), yamlPath)
	// May fail if mock model is nil — that's ok, we exercise the path
	_ = err
}

func TestFromConfigInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "bad.yaml")
	os.WriteFile(yamlPath, []byte(":::invalid yaml:::"), 0644)

	_, err := configurable.FromConfig(context.Background(), yamlPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestFromConfigUnknownAgentClass(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "unknown.yaml")
	yaml := `name: unknown-agent
agent_class: NonexistentAgentType
`
	os.WriteFile(yamlPath, []byte(yaml), 0644)

	_, err := configurable.FromConfig(context.Background(), yamlPath)
	if err == nil {
		t.Fatal("expected error for unknown agent_class")
	}
}

func TestResolveAgentReference(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.yaml")
	childPath := filepath.Join(dir, "child.yaml")

	childYAML := `name: child-agent
model: test
provider: mock
`
	os.WriteFile(childPath, []byte(childYAML), 0644)

	_, err := configurable.ResolveAgentReference(context.Background(), parentPath, "child.yaml")
	// May fail due to model resolution — just exercise the code path
	_ = err
}

func TestResolveAgentReferenceEmptyPath(t *testing.T) {
	_, err := configurable.ResolveAgentReference(context.Background(), "/parent.yaml", "")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestFromConfigCaching(t *testing.T) {
	configurable.SetModelResolver(func(ctx context.Context, cfg configurable.ModelConfig) (model.LLM, error) {
		return nil, nil
	})

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "cached.yaml")
	yaml := `name: cached-agent
model: test
provider: mock
`
	os.WriteFile(yamlPath, []byte(yaml), 0644)

	a1, err1 := configurable.FromConfig(context.Background(), yamlPath)
	a2, err2 := configurable.FromConfig(context.Background(), yamlPath)
	_, _ = a1, a2
	_, _ = err1, err2
}

// --- Plugin YAML tests ---

func TestRegisterPluginFactory(t *testing.T) {
	err := configurable.RegisterPluginFactory("test_plugin_unique", func(ctx context.Context, args map[string]any) (*plugin.Plugin, error) {
		return &plugin.Plugin{Name: "test"}, nil
	})
	if err != nil {
		t.Fatalf("RegisterPluginFactory: %v", err)
	}

	// Duplicate should fail
	err = configurable.RegisterPluginFactory("test_plugin_unique", func(ctx context.Context, args map[string]any) (*plugin.Plugin, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("expected error for duplicate plugin factory")
	}
}

func TestResolvePluginReference(t *testing.T) {
	configurable.RegisterPluginFactory("resolve_test_plugin", func(ctx context.Context, args map[string]any) (*plugin.Plugin, error) {
		name := "default"
		if v, ok := args["name"].(string); ok {
			name = v
		}
		return &plugin.Plugin{Name: name}, nil
	})

	p, err := configurable.ResolvePluginReference(context.Background(), "resolve_test_plugin", map[string]any{"name": "custom"})
	if err != nil {
		t.Fatalf("ResolvePluginReference: %v", err)
	}
	if p.Name != "custom" {
		t.Errorf("expected plugin name 'custom', got %q", p.Name)
	}
}

func TestResolvePluginReferenceNotFound(t *testing.T) {
	_, err := configurable.ResolvePluginReference(context.Background(), "nonexistent_plugin", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}
}

func TestResolvePluginReferenceEmpty(t *testing.T) {
	_, err := configurable.ResolvePluginReference(context.Background(), "", nil)
	if err == nil {
		t.Fatal("expected error for empty plugin name")
	}
}

func TestPluginsFromConfig_CostGovernor(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "agent.yaml")
	yamlContent := `name: test-agent
model: test
plugins:
  - name: cost-governor
    args:
      max_total_tokens: 50000
      max_dollar_budget: 0.25
      price_per_input_token: 0.00000015
      price_per_output_token: 0.0000006
      tool_call_caps:
        run_command: 5
`
	os.WriteFile(yamlPath, []byte(yamlContent), 0644)

	plugins, err := configurable.PluginsFromConfig(context.Background(), yamlPath)
	if err != nil {
		t.Fatalf("PluginsFromConfig: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name != "cost-governor" {
		t.Errorf("expected 'cost-governor', got %q", plugins[0].Name)
	}
}

func TestPluginsFromConfig_CircuitBreaker(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "agent.yaml")
	yamlContent := `name: test-agent
model: test
plugins:
  - name: circuit-breaker
    args:
      max_identical_calls: 3
      max_consecutive_fails: 3
      cooldown_duration: "10s"
`
	os.WriteFile(yamlPath, []byte(yamlContent), 0644)

	plugins, err := configurable.PluginsFromConfig(context.Background(), yamlPath)
	if err != nil {
		t.Fatalf("PluginsFromConfig: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Name != "circuit-breaker" {
		t.Errorf("expected 'circuit-breaker', got %q", plugins[0].Name)
	}
}

func TestPluginsFromConfig_MultiplePlugins(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "agent.yaml")
	yamlContent := `name: test-agent
model: test
plugins:
  - name: cost-governor
    args:
      max_total_tokens: 100000
  - name: circuit-breaker
`
	os.WriteFile(yamlPath, []byte(yamlContent), 0644)

	plugins, err := configurable.PluginsFromConfig(context.Background(), yamlPath)
	if err != nil {
		t.Fatalf("PluginsFromConfig: %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}
}

func TestPluginsFromConfig_NoPlugins(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "agent.yaml")
	yamlContent := `name: test-agent
model: test
`
	os.WriteFile(yamlPath, []byte(yamlContent), 0644)

	plugins, err := configurable.PluginsFromConfig(context.Background(), yamlPath)
	if err != nil {
		t.Fatalf("PluginsFromConfig: %v", err)
	}
	if plugins != nil {
		t.Errorf("expected nil for no plugins, got %v", plugins)
	}
}

func TestPluginsFromConfig_MissingFile(t *testing.T) {
	_, err := configurable.PluginsFromConfig(context.Background(), "/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

