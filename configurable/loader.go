package configurable

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/llmagent"
	"github.com/formonkey/moa/agent/workflowagents/loopagent"
	"github.com/formonkey/moa/agent/workflowagents/parallelagent"
	"github.com/formonkey/moa/agent/workflowagents/sequentialagent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/plugin/circuitbreaker"
	"github.com/formonkey/moa/plugin/costgovernor"
	"github.com/formonkey/moa/plugin/teacherescalation"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/codegraph"
	"github.com/formonkey/moa/tool/scraper"
)

func init() {
	// Register built-in agent factories
	must(RegisterAgent("LlmAgent", newLLMAgent))
	must(RegisterAgent("LoopAgent", newLoopAgent))
	must(RegisterAgent("ParallelAgent", newParallelAgent))
	must(RegisterAgent("SequentialAgent", newSequentialAgent))

	// Register built-in tool factories
	must(RegisterToolFactory("scrape_url", newScrapeTool))
	must(RegisterToolsetFactory("codegraph", newCodegraphToolset))

	// Register built-in plugin factories
	must(RegisterPluginFactory("cost-governor", newCostGovernorPlugin))
	must(RegisterPluginFactory("circuit-breaker", newCircuitBreakerPlugin))
	must(RegisterPluginFactory("teacher-escalation", newTeacherEscalationPlugin))
}

// newScrapeTool creates a scrape_url tool from YAML args.
//
// YAML example:
//
//	tools:
//	  - name: scrape_url
//	    args:
//	      user_agent: "MyBot/1.0"
//	      timeout: "30s"
//	      max_body_size: 5242880
func newScrapeTool(_ context.Context, args map[string]any) (tool.Tool, error) {
	cfg := scraper.Config{}
	if v, ok := args["user_agent"].(string); ok {
		cfg.UserAgent = v
	}
	if v, ok := args["timeout"].(string); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("configurable: invalid scrape_url timeout %q: %w", v, err)
		}
		cfg.Timeout = d
	}
	if v, ok := args["max_body_size"].(int); ok {
		cfg.MaxBodySize = int64(v)
	}
	return scraper.NewScrapeTool(cfg), nil
}

// newCodegraphToolset creates a codegraph toolset from YAML args.
//
// YAML example:
//
//	tools:
//	  - name: codegraph
//	    args:
//	      project_dir: "./"
//	      codegraph_bin: "codegraph"
func newCodegraphToolset(_ context.Context, args map[string]any) (tool.Toolset, error) {
	cfg := codegraph.Config{}
	if v, ok := args["project_dir"].(string); ok {
		cfg.ProjectDir = v
	}
	if v, ok := args["codegraph_bin"].(string); ok {
		cfg.CodegraphBin = v
	}
	return codegraph.NewToolset(cfg)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// FromConfig builds an agent tree from a YAML config file path.
// This is the primary entry point for declarative agent construction.
//
// Example usage:
//
//	agent, err := configurable.FromConfig(ctx, "./agents/root.yaml")
//	runner, _ := runner.New(runner.Config{Agent: agent, ...})
func FromConfig(ctx context.Context, configPath string) (agent.Agent, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("configurable: failed to resolve path: %w", err)
	}

	// Check cache first
	if cached, ok := getCachedAgent(absPath); ok {
		return cached, nil
	}

	// Read file
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("configurable: config file not found: %s", absPath)
		}
		return nil, err
	}

	// Peek at agent_class to determine which factory to use
	var base BaseAgentConfig
	if err := yaml.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("configurable: invalid YAML: %w", err)
	}

	agentClass := base.AgentClass
	if agentClass == "" {
		agentClass = "LlmAgent" // default
	}

	// Lookup factory
	registryMu.RLock()
	factory, exists := agentFactories[agentClass]
	registryMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("configurable: unknown agent_class %q (registered: %v)", agentClass, registeredAgentClasses())
	}

	// Build agent
	a, err := factory(ctx, data, absPath)
	if err != nil {
		return nil, err
	}

	// Cache for sub-agent dedup
	cacheAgent(absPath, a)
	return a, nil
}

// ResolveAgentReference resolves a sub-agent from a config_path reference.
// Handles relative paths relative to the parent config file.
func ResolveAgentReference(ctx context.Context, parentPath, refPath string) (agent.Agent, error) {
	if refPath == "" {
		return nil, fmt.Errorf("configurable: agent reference path cannot be empty")
	}

	targetPath := refPath
	if !filepath.IsAbs(refPath) {
		targetPath = filepath.Join(filepath.Dir(parentPath), refPath)
	}

	return FromConfig(ctx, targetPath)
}

func registeredAgentClasses() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	classes := make([]string, 0, len(agentFactories))
	for k := range agentFactories {
		classes = append(classes, k)
	}
	return classes
}

// --- Built-in agent factories ---

func newLLMAgent(ctx context.Context, data []byte, configPath string) (agent.Agent, error) {
	var cfg LLMAgentYAMLConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("configurable: failed to parse LlmAgent config: %w", err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("configurable: 'name' is required for LlmAgent")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("configurable: 'model' is required for LlmAgent")
	}
	cfg.ConfigPath = configPath

	// Resolve sub-agents
	subAgents, err := resolveSubAgents(ctx, configPath, cfg.SubAgents)
	if err != nil {
		return nil, err
	}

	// Resolve tools
	tools, toolsets, err := resolveTools(ctx, cfg.Tools)
	if err != nil {
		return nil, err
	}

	// Resolve callbacks
	beforeCBs, err := resolveCallbacks[agent.BeforeAgentCallback](ctx, cfg.BeforeAgentCallbacks)
	if err != nil {
		return nil, err
	}
	afterCBs, err := resolveCallbacks[agent.AfterAgentCallback](ctx, cfg.AfterAgentCallbacks)
	if err != nil {
		return nil, err
	}

	// Build model — uses the ModelResolver hook if registered, otherwise error
	llm, err := resolveModel(ctx, cfg.Provider, cfg.Model, cfg.Endpoint, cfg.APIKey)
	if err != nil {
		return nil, fmt.Errorf("configurable: failed to resolve model %q (provider=%q): %w", cfg.Model, cfg.Provider, err)
	}

	return llmagent.New(llmagent.Config{
		Name:                     cfg.Name,
		Description:              cfg.Description,
		Model:                    llm,
		Instruction:              cfg.Instruction,
		SubAgents:                subAgents,
		Tools:                    tools,
		Toolsets:                 toolsets,
		OutputKey:                cfg.OutputKey,
		DisallowTransferToPeers:  cfg.DisallowTransferToPeers,
		DisallowTransferToParent: cfg.DisallowTransferToParent,
		GenerateContentConfig:    cfg.GenerateContentConfig,
		BeforeAgentCallbacks:     beforeCBs,
		AfterAgentCallbacks:      afterCBs,
	})
}

func newLoopAgent(ctx context.Context, data []byte, configPath string) (agent.Agent, error) {
	var cfg LoopAgentYAMLConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("configurable: failed to parse LoopAgent config: %w", err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("configurable: 'name' is required for LoopAgent")
	}
	cfg.ConfigPath = configPath

	subAgents, err := resolveSubAgents(ctx, configPath, cfg.SubAgents)
	if err != nil {
		return nil, err
	}
	if len(subAgents) == 0 {
		return nil, fmt.Errorf("configurable: LoopAgent %q requires at least one sub_agent", cfg.Name)
	}

	return loopagent.New(loopagent.Config{
		Name:          cfg.Name,
		Description:   cfg.Description,
		SubAgent:      subAgents[0],
		MaxIterations: cfg.MaxIterations,
	})
}

func newParallelAgent(ctx context.Context, data []byte, configPath string) (agent.Agent, error) {
	var cfg ParallelAgentYAMLConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("configurable: failed to parse ParallelAgent config: %w", err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("configurable: 'name' is required for ParallelAgent")
	}
	cfg.ConfigPath = configPath

	subAgents, err := resolveSubAgents(ctx, configPath, cfg.SubAgents)
	if err != nil {
		return nil, err
	}

	return parallelagent.New(parallelagent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		SubAgents:   subAgents,
	})
}

func newSequentialAgent(ctx context.Context, data []byte, configPath string) (agent.Agent, error) {
	var cfg SequentialAgentYAMLConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("configurable: failed to parse SequentialAgent config: %w", err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("configurable: 'name' is required for SequentialAgent")
	}
	cfg.ConfigPath = configPath

	subAgents, err := resolveSubAgents(ctx, configPath, cfg.SubAgents)
	if err != nil {
		return nil, err
	}

	return sequentialagent.New(sequentialagent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		SubAgents:   subAgents,
	})
}

// --- Resolvers ---

func resolveSubAgents(ctx context.Context, parentPath string, refs []AgentRefConfig) ([]agent.Agent, error) {
	var agents []agent.Agent
	for _, ref := range refs {
		if ref.ConfigPath != "" {
			a, err := ResolveAgentReference(ctx, parentPath, ref.ConfigPath)
			if err != nil {
				return nil, fmt.Errorf("configurable: failed to resolve sub-agent %q: %w", ref.ConfigPath, err)
			}
			agents = append(agents, a)
		} else if len(ref.Inline) > 0 {
			a, err := resolveInlineAgent(ctx, parentPath, ref.Inline)
			if err != nil {
				name, _ := ref.Inline["name"].(string)
				return nil, fmt.Errorf("configurable: failed to resolve inline sub-agent %q: %w", name, err)
			}
			agents = append(agents, a)
		} else if ref.Code != "" {
			return nil, fmt.Errorf("configurable: inline code agent references not yet supported: %q", ref.Code)
		}
	}
	return agents, nil
}

// resolveInlineAgent creates an agent from an inline map definition.
// It marshals the map back to YAML bytes and feeds it through the normal factory system.
func resolveInlineAgent(ctx context.Context, parentPath string, inline map[string]any) (agent.Agent, error) {
	data, err := yaml.Marshal(inline)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inline agent: %w", err)
	}

	// Determine agent class
	var base BaseAgentConfig
	if err := yaml.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("failed to parse inline agent base config: %w", err)
	}

	agentClass := base.AgentClass
	if agentClass == "" {
		agentClass = "LlmAgent"
	}

	factory, ok := getAgentFactory(agentClass)
	if !ok {
		return nil, fmt.Errorf("unknown agent_class %q", agentClass)
	}

	return factory(ctx, data, parentPath)
}

func resolveTools(ctx context.Context, toolConfigs []ToolConfig) ([]tool.Tool, []tool.Toolset, error) {
	var tools []tool.Tool
	var toolsets []tool.Toolset
	for _, tc := range toolConfigs {
		if tc.Name == "" {
			continue
		}
		t, ts, err := ResolveToolReference(ctx, tc.Name, tc.Args)
		if err != nil {
			return nil, nil, fmt.Errorf("configurable: failed to resolve tool %q: %w", tc.Name, err)
		}
		if t != nil {
			tools = append(tools, t)
		}
		if ts != nil {
			toolsets = append(toolsets, ts)
		}
	}
	return tools, toolsets, nil
}

func resolveCallbacks[T any](ctx context.Context, configs []CodeConfig) ([]T, error) {
	var cbs []T
	for _, ref := range configs {
		if ref.Name == "" {
			continue
		}
		c, err := ResolveCallbackReference(ctx, ref.Name)
		if err != nil {
			return nil, fmt.Errorf("configurable: failed to resolve callback %q: %w", ref.Name, err)
		}
		cb, ok := c.(T)
		if !ok {
			return nil, fmt.Errorf("configurable: callback %q is %T, expected %T", ref.Name, c, *new(T))
		}
		cbs = append(cbs, cb)
	}
	return cbs, nil
}

// --- Model resolution ---

// ModelConfig contains all the configuration needed to create a model instance.
type ModelConfig struct {
	Provider string // e.g., "gemini", "ollama", "openai"
	Model    string // e.g., "gemini-2.5-flash", "qwen3:8b"
	Endpoint string // Optional: base URL (e.g., "http://gpu-server:11434")
	APIKey   string // Optional: API key (resolved from ${ENV_VAR} refs)
}

// ModelResolver is a function that creates an LLM from a model configuration.
// Users register their own resolver to support their preferred model providers.
type ModelResolver func(ctx context.Context, cfg ModelConfig) (model.LLM, error)

var (
	modelResolverMu sync.RWMutex
	modelResolver   ModelResolver
)

// SetModelResolver registers a function that creates LLM instances from model config.
// This MUST be called before FromConfig if your YAML configs reference models.
//
// Example:
//
//	configurable.SetModelResolver(func(ctx context.Context, cfg configurable.ModelConfig) (model.LLM, error) {
//	    switch cfg.Provider {
//	    case "ollama":
//	        return ollama.NewClient(cfg.Endpoint, cfg.Model), nil
//	    case "gemini":
//	        apiKey := cfg.APIKey
//	        if apiKey == "" { apiKey = os.Getenv("GEMINI_API_KEY") }
//	        return gemini.NewClient(ctx, apiKey, cfg.Model)
//	    default:
//	        return nil, fmt.Errorf("unknown provider: %s", cfg.Provider)
//	    }
//	})
func SetModelResolver(resolver ModelResolver) {
	modelResolverMu.Lock()
	defer modelResolverMu.Unlock()
	modelResolver = resolver
}

func resolveModel(ctx context.Context, provider, modelName, endpoint, apiKey string) (model.LLM, error) {
	modelResolverMu.RLock()
	resolver := modelResolver
	modelResolverMu.RUnlock()

	if resolver == nil {
		return nil, fmt.Errorf("configurable: no ModelResolver registered — call configurable.SetModelResolver() before FromConfig()")
	}

	// Resolve ${ENV_VAR} references in api_key
	if strings.HasPrefix(apiKey, "${") && strings.HasSuffix(apiKey, "}") {
		envName := apiKey[2 : len(apiKey)-1]
		apiKey = os.Getenv(envName)
	}

	return resolver(ctx, ModelConfig{
		Provider: provider,
		Model:    modelName,
		Endpoint: endpoint,
		APIKey:   apiKey,
	})
}

// --- Plugin factories ---

// newCostGovernorPlugin creates a cost-governor plugin from YAML args.
//
// YAML example:
//
//	plugins:
//	  - name: cost-governor
//	    args:
//	      max_total_tokens: 100000
//	      max_wall_time: "5m"
//	      max_dollar_budget: 0.50
//	      price_per_input_token: 0.00000015
//	      price_per_output_token: 0.0000006
//	      max_dollars_per_minute: 0.10
//	      tool_call_caps:
//	        run_command: 10
func newCostGovernorPlugin(_ context.Context, args map[string]any) (*plugin.Plugin, error) {
	cfg := costgovernor.Config{}
	if v, ok := toInt(args["max_total_tokens"]); ok {
		cfg.MaxTotalTokens = v
	}
	if v, ok := toFloat(args["max_dollar_budget"]); ok {
		cfg.MaxDollarBudget = v
	}
	if v, ok := toFloat(args["price_per_input_token"]); ok {
		cfg.PricePerInputToken = v
	}
	if v, ok := toFloat(args["price_per_output_token"]); ok {
		cfg.PricePerOutputToken = v
	}
	if v, ok := toFloat(args["max_dollars_per_minute"]); ok {
		cfg.MaxDollarsPerMinute = v
	}
	if v, ok := args["max_wall_time"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.MaxWallTime = d
		}
	}
	if caps, ok := args["tool_call_caps"].(map[string]any); ok {
		cfg.ToolCallCaps = make(map[string]int)
		for name, val := range caps {
			if v, ok := toInt(val); ok {
				cfg.ToolCallCaps[name] = v
			}
		}
	}
	return costgovernor.New(cfg), nil
}

// newCircuitBreakerPlugin creates a circuit-breaker plugin from YAML args.
//
// YAML example:
//
//	plugins:
//	  - name: circuit-breaker
//	    args:
//	      max_identical_calls: 5
//	      max_consecutive_fails: 5
//	      cooldown_duration: "30s"
func newCircuitBreakerPlugin(_ context.Context, args map[string]any) (*plugin.Plugin, error) {
	cfg := circuitbreaker.Config{}
	if v, ok := toInt(args["max_identical_calls"]); ok {
		cfg.MaxIdenticalCalls = v
	}
	if v, ok := toInt(args["max_consecutive_fails"]); ok {
		cfg.MaxConsecutiveFails = v
	}
	if v, ok := args["cooldown_duration"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.CooldownDuration = d
		}
	}
	if v, ok := toInt(args["max_probes"]); ok {
		cfg.MaxProbes = v
	}
	return circuitbreaker.New(cfg), nil
}

// newTeacherEscalationPlugin creates a teacher-escalation plugin from YAML args.
//
// YAML example:
//
//	plugins:
//	  - name: teacher-escalation
//	    args:
//	      teacher_model: gpt-4o
//	      teacher_provider: openai
//	      min_confidence: 0.6
func newTeacherEscalationPlugin(ctx context.Context, args map[string]any) (*plugin.Plugin, error) {
	cfg := teacherescalation.Config{}

	// Resolve teacher model using the registered ModelResolver.
	teacherModelName, _ := args["teacher_model"].(string)
	teacherProvider, _ := args["teacher_provider"].(string)

	if teacherModelName != "" {
		teacherEndpoint, _ := args["teacher_endpoint"].(string)
		teacherAPIKey, _ := args["teacher_api_key"].(string)
		teacherLLM, err := resolveModel(ctx, teacherProvider, teacherModelName, teacherEndpoint, teacherAPIKey)
		if err != nil {
			return nil, fmt.Errorf("configurable: failed to resolve teacher model %q: %w", teacherModelName, err)
		}
		cfg.TeacherModel = teacherLLM
	}

	if v, ok := toFloat(args["min_confidence"]); ok {
		cfg.MinConfidence = v
	}

	return teacherescalation.New(cfg), nil
}

// --- Plugin resolver ---

// resolvePlugins builds plugins from YAML configs.
func resolvePlugins(ctx context.Context, configs []PluginConfig) ([]*plugin.Plugin, error) {
	var plugins []*plugin.Plugin
	for _, pc := range configs {
		if pc.Name == "" {
			continue
		}
		p, err := ResolvePluginReference(ctx, pc.Name, pc.Args)
		if err != nil {
			return nil, fmt.Errorf("configurable: failed to resolve plugin %q: %w", pc.Name, err)
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

// PluginsFromConfig extracts plugin configurations from a YAML agent config file
// and returns the resolved plugins. These can be passed to runner.Config.Plugins.
//
// Usage:
//
//	agent, _ := configurable.FromConfig(ctx, "./agent.yaml")
//	plugins, _ := configurable.PluginsFromConfig(ctx, "./agent.yaml")
//	runner, _ := runner.New(runner.Config{
//	    Agent:   agent,
//	    Plugins: plugins,
//	})
func PluginsFromConfig(ctx context.Context, configPath string) ([]*plugin.Plugin, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("configurable: failed to resolve path: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("configurable: failed to read config: %w", err)
	}

	var base BaseAgentConfig
	if err := yaml.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("configurable: invalid YAML: %w", err)
	}

	if len(base.Plugins) == 0 {
		return nil, nil
	}

	return resolvePlugins(ctx, base.Plugins)
}

// --- Type conversion helpers ---

// toInt converts interface{} to int, handling YAML's tendency to decode
// numbers as int or float64.
func toInt(v any) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	default:
		return 0, false
	}
}

// toFloat converts interface{} to float64.
func toFloat(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}
