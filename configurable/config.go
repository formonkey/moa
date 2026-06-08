package configurable

import (
	"google.golang.org/genai"
)

// --- YAML Config structs ---

// CodeConfig represents a reference to a callback function registered in the registry.
type CodeConfig struct {
	// Name of the registered callback (e.g., "mypackage.SecurityCheck")
	Name   string         `yaml:"name" json:"name"`
	Params map[string]any `yaml:"params,omitempty" json:"params,omitempty"`
}

// AgentRefConfig represents a reference to a sub-agent, either by config file path or inline.
//
// Use config_path to reference a separate YAML file:
//
//	sub_agents:
//	  - config_path: frontend.yaml
//
// Use inline to define the agent directly in the parent config:
//
//	sub_agents:
//	  - inline:
//	      name: frontend
//	      model: qwen3:8b
//	      provider: ollama
//	      instruction: "You are a frontend dev"
type AgentRefConfig struct {
	// Path to another agent's YAML/JSON file (relative to parent config)
	ConfigPath string `yaml:"config_path,omitempty" json:"config_path,omitempty"`
	// Inline agent definition — full agent config embedded directly
	Inline map[string]any `yaml:"inline,omitempty" json:"inline,omitempty"`
	// Code reference (for future use)
	Code string `yaml:"code,omitempty" json:"code,omitempty"`
}

// ToolConfig represents a tool reference in YAML.
type ToolConfig struct {
	Name string         `yaml:"name" json:"name"`
	Args map[string]any `yaml:"args,omitempty" json:"args,omitempty"`
}

// PluginConfig represents a plugin reference in YAML.
//
// YAML example:
//
//	plugins:
//	  - name: cost-governor
//	    args:
//	      max_total_tokens: 100000
//	      max_dollar_budget: 0.50
//	  - name: circuit-breaker
//	  - name: teacher-escalation
//	    args:
//	      teacher_model: gpt-4o
//	      teacher_provider: openai
type PluginConfig struct {
	Name string         `yaml:"name" json:"name"`
	Args map[string]any `yaml:"args,omitempty" json:"args,omitempty"`
}

// BaseAgentConfig contains fields shared by all agent types.
type BaseAgentConfig struct {
	// AgentClass determines which factory to use (e.g., "LlmAgent", "LoopAgent").
	// Defaults to "LlmAgent" if not specified.
	AgentClass string `yaml:"agent_class" json:"agent_class"`

	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	SubAgents []AgentRefConfig `yaml:"sub_agents,omitempty" json:"sub_agents,omitempty"`

	BeforeAgentCallbacks []CodeConfig   `yaml:"before_agent_callbacks,omitempty" json:"before_agent_callbacks,omitempty"`
	AfterAgentCallbacks  []CodeConfig   `yaml:"after_agent_callbacks,omitempty" json:"after_agent_callbacks,omitempty"`
	Plugins              []PluginConfig `yaml:"plugins,omitempty" json:"plugins,omitempty"`

	// ConfigPath is set internally during resolution (not from YAML).
	ConfigPath string `yaml:"-" json:"-"`
}

// LLMAgentYAMLConfig is the YAML config for an LLM-powered agent.
type LLMAgentYAMLConfig struct {
	BaseAgentConfig `yaml:",inline" json:",inline"`

	// Model identifier (e.g., "gemini-2.0-flash", "gpt-4o", "qwen-plus")
	Model string `yaml:"model" json:"model"`
	// Provider name (e.g., "gemini", "openai", "anthropic", "ollama")
	// If empty, inferred from model name.
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	// Endpoint is the base URL for the model API (e.g., "http://gpu-server:11434").
	// Optional — if empty, each provider uses its default.
	Endpoint string `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	// APIKey for the model provider. Supports env var references: "${GEMINI_API_KEY}".
	// Optional — if empty, the resolver reads from default env vars.
	APIKey string `yaml:"api_key,omitempty" json:"api_key,omitempty"`

	Instruction string `yaml:"instruction" json:"instruction"`

	Tools []ToolConfig `yaml:"tools,omitempty" json:"tools,omitempty"`

	DisallowTransferToPeers  bool `yaml:"disallow_transfer_to_peers,omitempty" json:"disallow_transfer_to_peers,omitempty"`
	DisallowTransferToParent bool `yaml:"disallow_transfer_to_parent,omitempty" json:"disallow_transfer_to_parent,omitempty"`

	// OutputKey saves the agent's final text output to session state under this key.
	OutputKey string `yaml:"output_key,omitempty" json:"output_key,omitempty"`

	// GenerateContentConfig is the raw genai config for the model.
	GenerateContentConfig *genai.GenerateContentConfig `yaml:"generate_content_config,omitempty" json:"generate_content_config,omitempty"`
}

// LoopAgentYAMLConfig is the YAML config for a loop workflow agent.
type LoopAgentYAMLConfig struct {
	BaseAgentConfig `yaml:",inline" json:",inline"`
	MaxIterations   int `yaml:"max_iterations" json:"max_iterations"`
}

// ParallelAgentYAMLConfig is the YAML config for a parallel workflow agent.
type ParallelAgentYAMLConfig struct {
	BaseAgentConfig `yaml:",inline" json:",inline"`
}

// SequentialAgentYAMLConfig is the YAML config for a sequential workflow agent.
type SequentialAgentYAMLConfig struct {
	BaseAgentConfig `yaml:",inline" json:",inline"`
}

// CompetitiveAgentYAMLConfig is for the competitive (voting) workflow agent.
type CompetitiveAgentYAMLConfig struct {
	BaseAgentConfig `yaml:",inline" json:",inline"`
	// Number of clones to run (default: 2)
	Clones int `yaml:"clones,omitempty" json:"clones,omitempty"`
}

// DebateAgentYAMLConfig is for the debate workflow agent.
type DebateAgentYAMLConfig struct {
	BaseAgentConfig `yaml:",inline" json:",inline"`
}
