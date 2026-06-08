// Package swarmconfig provides declarative YAML-based swarm orchestration for go-brain-v2.
//
// It reads a swarm.yaml file (compatible with go-brain v1 recipes) and builds
// the full agent tree using v2 primitives (llmagent, sequentialagent, parallelagent, etc.).
//
// Usage:
//
//	swarm, err := swarmconfig.FromYAML(ctx, "./swarm.yaml")
//	runner, _ := runner.New(runner.Config{Agent: swarm.RootAgent, ...})
package swarmconfig

import (
	"context"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/llmagent"
	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/rag"
	"github.com/formonkey/moa/swarm/fsmagent"
	"github.com/formonkey/moa/model/anthropic"
	"github.com/formonkey/moa/model/deepseek"
	"github.com/formonkey/moa/model/gemini"
	"github.com/formonkey/moa/model/groq"
	"github.com/formonkey/moa/model/kimi"
	"github.com/formonkey/moa/model/mistral"
	"github.com/formonkey/moa/model/ollama"
	"github.com/formonkey/moa/model/openai"
	"github.com/formonkey/moa/model/openrouter"
	"github.com/formonkey/moa/model/qwen"
	"github.com/formonkey/moa/swarm/blackboard"
	"github.com/formonkey/moa/swarm/scheduler"
	"github.com/formonkey/moa/tool"
)

// --- YAML Schema (compatible with go-brain v1 swarm.yaml) ---

// Config is the root YAML structure.
type Config struct {
	Swarm SwarmDef `yaml:"swarm"`
}

// SwarmDef holds the swarm topology definition.
type SwarmDef struct {
	ProjectID  string            `yaml:"project_id"`
	Flow       string            `yaml:"flow"`        // "hierarchical", "competitive", "debate"
	MaxWorkers int               `yaml:"max_workers"`
	Store      StoreDef          `yaml:"store"`
	MCPs       map[string]MCPDef `yaml:"mcps"`
	Agents     []AgentDef        `yaml:"agents"`
}

// StoreDef configures the persistence backend.
type StoreDef struct {
	Provider string `yaml:"provider"`
	Path     string `yaml:"path"`
}

// MCPDef defines an MCP server connection.
type MCPDef struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// RAGDef defines RAG sources for an agent.
type RAGDef struct {
	Storage string   `yaml:"storage"` // Badger DB path. Defaults to "./.moa_rag"
	Files   []string `yaml:"files"`
	MCPs    []string `yaml:"mcps"`
}

// WatchDef defines a file-watch rule for reactive agents.
type WatchDef struct {
	Path  string `yaml:"path"`
	Event string `yaml:"event"`
	Diff  bool   `yaml:"diff"`
}

// SkillDef defines an inline skill.
type SkillDef struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Content     string `yaml:"content"`
}

// StateDef defines an FSM state node.
type StateDef struct {
	Name          string            `yaml:"name"`
	System        string            `yaml:"system"`
	Prompt        string            `yaml:"prompt"`
	MaxRetries    int               `yaml:"max_retries"`
	FallbackState string            `yaml:"fallback_state"`
	Options       []string          `yaml:"options"`
	Routes        map[string]string `yaml:"routes"`
	Transitions   []string          `yaml:"transitions"`
	Result        string            `yaml:"result"`
	Tools         []string          `yaml:"tools"` // per-state tools (overrides agent-level when set)
	DocSearch     *DocSearchDef     `yaml:"docsearch"`
}

// DocSearchDef configures documentation search via a librarian agent.
type DocSearchDef struct {
	Agent      string `yaml:"agent"`
	Prompt     string `yaml:"prompt"`
	TopK       int    `yaml:"top_k"`
	ContextVar string `yaml:"context_var"`
	Required   bool   `yaml:"required"`
}

// AgentDef defines a single agent in the swarm.
type AgentDef struct {
	Name        string     `yaml:"name"`
	Type        string     `yaml:"type"`
	Adapter     string     `yaml:"adapter"`
	Endpoint    string     `yaml:"endpoint"` // custom endpoint URL (e.g. llama-server)
	Model       string     `yaml:"model"`
	Master      bool       `yaml:"master"`
	HITL        bool       `yaml:"hitl"`
	Language    string     `yaml:"language"`
	MaxContext  int        `yaml:"max_context"`
	System      string     `yaml:"system"`
	Sandbox     string     `yaml:"sandbox"`
	Agents      []string   `yaml:"agents"`
	MCPs        []string   `yaml:"mcps"`
	Tools       []string   `yaml:"tools"`
	Triggers    []string   `yaml:"triggers"`
	Specialties []string   `yaml:"specialties"`
	RAGs        RAGDef     `yaml:"rags"`
	Watches     []WatchDef `yaml:"watches"`
	Skills      []SkillDef `yaml:"skills"`
	States      []StateDef `yaml:"states"`
}

// --- Swarm Result ---

// Swarm is the result of parsing a swarm.yaml file.
type Swarm struct {
	RootAgent  agent.Agent
	Agents     map[string]agent.Agent
	Blackboard *blackboard.Blackboard
	EventBus   *scheduler.EventBus
	Scheduler  *scheduler.Scheduler
	Config     *Config
}

// --- Parser ---

// FromYAML reads a swarm.yaml file and builds the complete agent tree.
func FromYAML(ctx context.Context, filePath string) (*Swarm, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("swarmconfig: failed to read %s: %w", filePath, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("swarmconfig: failed to parse YAML: %w", err)
	}
	return buildSwarm(ctx, &cfg)
}

// FromBytes builds a swarm from raw YAML bytes.
func FromBytes(ctx context.Context, data []byte) (*Swarm, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("swarmconfig: failed to parse YAML: %w", err)
	}
	return buildSwarm(ctx, &cfg)
}

func buildSwarm(ctx context.Context, cfg *Config) (*Swarm, error) {
	bb := blackboard.New()
	agentMap := make(map[string]agent.Agent)
	var masterAgent agent.Agent

	// Collect LLMs and agent defs that need FSM (for 2-pass wiring)
	type fsmPending struct {
		aDef AgentDef
		llm  model.LLM
	}
	var pendingFSM []fsmPending

	// Pass 1: Build all agents (FSM agents get a placeholder first)
	for _, aDef := range cfg.Swarm.Agents {
		llm, err := resolveAdapter(ctx, aDef.Adapter, aDef.Model, aDef.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("swarmconfig: agent %q: %w", aDef.Name, err)
		}

		instruction := aDef.System
		if len(aDef.Specialties) > 0 {
			instruction += "\n\nYour specialties: " + strings.Join(aDef.Specialties, ", ")
		}
		if aDef.Language != "" {
			instruction += "\n\nRespond in: " + aDef.Language
		}

		// Resolve tools from the configurable registry
		agentTools, agentToolsets, err := resolveAgentTools(ctx, aDef.Tools)
		if err != nil {
			return nil, fmt.Errorf("swarmconfig: agent %q: %w", aDef.Name, err)
		}

		// If agent has RAG files, create store, index, and inject search_docs tool
		if len(aDef.RAGs.Files) > 0 {
			ragStore, err := rag.NewStore(aDef.RAGs.Storage)
			if err != nil {
				return nil, fmt.Errorf("swarmconfig: agent %q: failed to create RAG store: %w", aDef.Name, err)
			}
			if err := ragStore.IndexFiles(aDef.RAGs.Files); err != nil {
				ragStore.Close()
				return nil, fmt.Errorf("swarmconfig: agent %q: failed to index RAG files: %w", aDef.Name, err)
			}
			searchDocsTool, err := rag.NewSearchDocsTool(ragStore)
			if err != nil {
				ragStore.Close()
				return nil, fmt.Errorf("swarmconfig: agent %q: failed to create search_docs tool: %w", aDef.Name, err)
			}
			agentTools = append(agentTools, searchDocsTool)

			// Register in global registry so per-state tool resolution finds it by name
			sdTool := searchDocsTool
			_ = configurable.RegisterToolFactory("search_docs", func(ctx context.Context, args map[string]any) (tool.Tool, error) {
				return sdTool, nil
			})
		}

		var builtAgent agent.Agent
		if len(aDef.States) > 0 {
			// Defer FSM building to Pass 1.5 (needs full agentMap for cross-agent)
			pendingFSM = append(pendingFSM, fsmPending{aDef: aDef, llm: llm})
			// Temporary placeholder
			builtAgent, _ = llmagent.New(llmagent.Config{
				Name:        aDef.Name,
				Description: fmt.Sprintf("%s agent (placeholder)", aDef.Name),
				Model:       llm,
				Instruction: instruction,
				Tools:       agentTools,
				Toolsets:    agentToolsets,
			})
		} else {
			builtAgent, err = llmagent.New(llmagent.Config{
				Name:        aDef.Name,
				Description: fmt.Sprintf("%s agent (%s)", aDef.Type, strings.Join(aDef.Specialties, ", ")),
				Model:       llm,
				Instruction: instruction,
				Tools:       agentTools,
				Toolsets:    agentToolsets,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("swarmconfig: failed to build agent %q: %w", aDef.Name, err)
		}

		agentMap[aDef.Name] = builtAgent
		if aDef.Master {
			masterAgent = builtAgent
		}
	}

	// Pass 1.5: Build FSM agents with full agentMap for cross-agent transitions
	for _, p := range pendingFSM {
		builtAgent, err := buildFSMAgent(ctx, p.aDef, p.llm, agentMap)
		if err != nil {
			return nil, fmt.Errorf("swarmconfig: failed to build FSM agent %q: %w", p.aDef.Name, err)
		}
		agentMap[p.aDef.Name] = builtAgent
		if p.aDef.Master {
			masterAgent = builtAgent
		}
	}

	// Pass 2: Wire sub-agent references (skip FSM agents — they use AgentMap for cross-agent)
	for _, aDef := range cfg.Swarm.Agents {
		if len(aDef.Agents) == 0 {
			continue
		}
		// FSM agents already have cross-agent support via AgentMap, don't rebuild them
		if len(aDef.States) > 0 {
			continue
		}
		var subAgents []agent.Agent
		for _, subName := range aDef.Agents {
			sub, ok := agentMap[subName]
			if !ok {
				return nil, fmt.Errorf("swarmconfig: agent %q references unknown sub-agent %q", aDef.Name, subName)
			}
			subAgents = append(subAgents, sub)
		}

		parentAgent := agentMap[aDef.Name]
		llm, _ := resolveAdapter(ctx, aDef.Adapter, aDef.Model, aDef.Endpoint)
		instruction := aDef.System
		if len(aDef.Specialties) > 0 {
			instruction += "\n\nYour specialties: " + strings.Join(aDef.Specialties, ", ")
		}

		// Re-resolve tools for the rebuilt agent
		rebuildTools, rebuildToolsets, _ := resolveAgentTools(ctx, aDef.Tools)

		rebuilt, err := llmagent.New(llmagent.Config{
			Name:        parentAgent.Name(),
			Description: parentAgent.Description(),
			Model:       llm,
			Instruction: instruction,
			SubAgents:   subAgents,
			Tools:       rebuildTools,
			Toolsets:    rebuildToolsets,
		})
		if err != nil {
			return nil, fmt.Errorf("swarmconfig: failed to rebuild agent %q with sub-agents: %w", aDef.Name, err)
		}
		agentMap[aDef.Name] = rebuilt
		if aDef.Master {
			masterAgent = rebuilt
		}
	}

	// Fallback master
	if masterAgent == nil {
		if len(agentMap) == 1 {
			for _, a := range agentMap {
				masterAgent = a
			}
		} else {
			return nil, fmt.Errorf("swarmconfig: no agent has 'master: true'")
		}
	}

	// Pass 3: Wire triggers to scheduler
	bus := scheduler.NewEventBus()
	var triggeredAgents []scheduler.AgentRunner
	for _, aDef := range cfg.Swarm.Agents {
		if len(aDef.Triggers) == 0 {
			continue
		}
		a, ok := agentMap[aDef.Name]
		if !ok {
			continue
		}
		triggeredAgents = append(triggeredAgents, scheduler.AgentRunner{
			Agent:    a,
			Triggers: aDef.Triggers,
		})
	}

	var sched *scheduler.Scheduler
	if len(triggeredAgents) > 0 {
		maxWorkers := cfg.Swarm.MaxWorkers
		if maxWorkers <= 0 {
			maxWorkers = 2
		}
		sched = scheduler.New(scheduler.Config{
			MaxWorkers: maxWorkers,
			Bus:        bus,
			Agents:     triggeredAgents,
		})
	}

	return &Swarm{
		RootAgent:  masterAgent,
		Agents:     agentMap,
		Blackboard: bb,
		EventBus:   bus,
		Scheduler:  sched,
		Config:     cfg,
	}, nil
}

// --- Built-in Model Resolution (10 adapters) ---

func resolveAdapter(ctx context.Context, adapter, modelName, endpoint string) (model.LLM, error) {
	switch adapter {
	case "ollama":
		return ollama.NewClient(endpoint, modelName), nil
	case "openai":
		key := os.Getenv("OPENAI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY not set")
		}
		return openai.NewClient(openai.Config{APIKey: key, Model: modelName}), nil
	case "anthropic":
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
		}
		return anthropic.NewClient(anthropic.Config{APIKey: key, Model: modelName}), nil
	case "gemini":
		key := os.Getenv("GEMINI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY not set")
		}
		return gemini.NewClient(ctx, key, modelName)
	case "deepseek":
		key := os.Getenv("DEEPSEEK_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("DEEPSEEK_API_KEY not set")
		}
		return deepseek.NewClient(deepseek.Config{APIKey: key, Model: modelName}), nil
	case "groq":
		key := os.Getenv("GROQ_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("GROQ_API_KEY not set")
		}
		return groq.NewClient(groq.Config{APIKey: key, Model: modelName}), nil
	case "kimi":
		key := os.Getenv("KIMI_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("KIMI_API_KEY not set")
		}
		return kimi.NewClient(kimi.Config{APIKey: key, Model: modelName}), nil
	case "mistral":
		key := os.Getenv("MISTRAL_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("MISTRAL_API_KEY not set")
		}
		return mistral.NewClient(mistral.Config{APIKey: key, Model: modelName}), nil
	case "openrouter":
		key := os.Getenv("OPENROUTER_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("OPENROUTER_API_KEY not set")
		}
		return openrouter.NewClient(openrouter.Config{APIKey: key, Model: modelName}), nil
	case "qwen":
		key := os.Getenv("QWEN_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("QWEN_API_KEY not set")
		}
		return qwen.NewClient(qwen.Config{APIKey: key, Model: modelName}), nil
	case "":
		return nil, fmt.Errorf("missing 'adapter' field")
	default:
		return nil, fmt.Errorf("unsupported adapter %q (available: ollama, openai, anthropic, gemini, deepseek, groq, kimi, mistral, openrouter, qwen)", adapter)
	}
}

// resolveAgentTools resolves tool references from YAML using the configurable registry.
func resolveAgentTools(ctx context.Context, toolNames []string) ([]tool.Tool, []tool.Toolset, error) {
	if len(toolNames) == 0 {
		return nil, nil, nil
	}
	var tools []tool.Tool
	var toolsets []tool.Toolset
	for _, name := range toolNames {
		t, ts, err := configurable.ResolveToolReference(ctx, name, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to resolve tool %q: %w", name, err)
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

// --- FSM Builder ---

func buildFSMAgent(ctx context.Context, aDef AgentDef, llm model.LLM, agentMap map[string]agent.Agent) (agent.Agent, error) {
	var states []fsmagent.StateConfig

	for _, state := range aDef.States {
		sc := fsmagent.StateConfig{
			Name:          state.Name,
			System:        state.System,
			Prompt:        state.Prompt,
			Options:       state.Options,
			Routes:        state.Routes,
			Transitions:   state.Transitions,
			ResultKey:     extractOutputKey(state.Result),
			MaxRetries:    state.MaxRetries,
			FallbackState: state.FallbackState,
		}

		// Resolve per-state tools
		if len(state.Tools) > 0 {
			stateTools, _, err := resolveAgentTools(ctx, state.Tools)
			if err != nil {
				return nil, fmt.Errorf("swarmconfig: agent %q state %q: %w", aDef.Name, state.Name, err)
			}
			sc.Tools = stateTools
		}

		// Wire DocSearch from YAML
		if state.DocSearch != nil {
			sc.DocSearch = &fsmagent.DocSearchConfig{
				AgentName:  state.DocSearch.Agent,
				Prompt:     state.DocSearch.Prompt,
				ContextVar: state.DocSearch.ContextVar,
				Required:   state.DocSearch.Required,
			}
		}

		states = append(states, sc)
	}

	desc := aDef.Type
	if len(aDef.Specialties) > 0 {
		desc += " (" + strings.Join(aDef.Specialties, ", ") + ")"
	}

	return fsmagent.New(fsmagent.Config{
		Name:        aDef.Name,
		Description: desc,
		Model:       llm,
		States:      states,
		AgentMap:    agentMap,
	})
}

// extractOutputKey converts "{{.result}}" → "result"
func extractOutputKey(tmpl string) string {
	s := strings.TrimPrefix(tmpl, "{{.")
	s = strings.TrimSuffix(s, "}}")
	if s == tmpl {
		return ""
	}
	return s
}
