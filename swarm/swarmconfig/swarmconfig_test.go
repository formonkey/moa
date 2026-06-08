package swarmconfig_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/formonkey/moa/swarm/swarmconfig"
)

func TestFromYAMLMissingFile(t *testing.T) {
	_, err := swarmconfig.FromYAML(context.Background(), "/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFromYAMLInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	os.WriteFile(path, []byte("{{{{invalid yaml!!!!"), 0644)

	_, err := swarmconfig.FromYAML(context.Background(), path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestFromBytesInvalidYAML(t *testing.T) {
	_, err := swarmconfig.FromBytes(context.Background(), []byte(":::not yaml"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestFromBytesUnsupportedAdapter(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: agent1
      adapter: unsupported_adapter
      model: test
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for unsupported adapter")
	}
}

func TestFromBytesMissingAdapter(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: agent1
      adapter: ""
      model: test
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for empty adapter")
	}
}

func TestFromBytesOllamaAdapter(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: agent1
      adapter: ollama
      model: llama3
      system: "You are helpful"
      master: true
`)
	swarm, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if swarm.RootAgent == nil {
		t.Fatal("expected root agent")
	}
	if swarm.RootAgent.Name() != "agent1" {
		t.Fatalf("expected 'agent1', got %q", swarm.RootAgent.Name())
	}
	if swarm.Blackboard == nil {
		t.Fatal("expected blackboard")
	}
	if swarm.Config == nil {
		t.Fatal("expected config")
	}
}

func TestFromBytesSingleAgentAutoMaster(t *testing.T) {
	// Single agent without master: true should become master automatically
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: solo
      adapter: ollama
      model: llama3
      system: "Solo"
`)
	swarm, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if swarm.RootAgent.Name() != "solo" {
		t.Fatalf("expected 'solo' as auto-master, got %q", swarm.RootAgent.Name())
	}
}

func TestFromBytesNoMasterMultipleAgents(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: a1
      adapter: ollama
      model: m1
      system: "Agent 1"
    - name: a2
      adapter: ollama
      model: m2
      system: "Agent 2"
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error: no agent has master: true")
	}
}

func TestFromBytesSubAgentReferences(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: worker
      adapter: ollama
      model: llama3
      system: "Worker"
    - name: manager
      adapter: ollama
      model: llama3
      system: "Manager"
      master: true
      agents:
        - worker
`)
	swarm, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if swarm.RootAgent.Name() != "manager" {
		t.Fatalf("expected 'manager', got %q", swarm.RootAgent.Name())
	}
	subs := swarm.RootAgent.SubAgents()
	if len(subs) != 1 {
		t.Fatalf("expected 1 sub-agent, got %d", len(subs))
	}
	if subs[0].Name() != "worker" {
		t.Fatalf("expected sub-agent 'worker', got %q", subs[0].Name())
	}
}

func TestFromBytesUnknownSubAgent(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: manager
      adapter: ollama
      model: llama3
      system: "Manager"
      master: true
      agents:
        - nonexistent
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for unknown sub-agent reference")
	}
}

func TestFromBytesWithSpecialtiesAndLanguage(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: specialist
      adapter: ollama
      model: llama3
      system: "You are an expert"
      master: true
      specialties:
        - Go
        - Python
      language: Spanish
`)
	swarm, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if swarm.RootAgent == nil {
		t.Fatal("expected root agent")
	}
}

func TestFromBytesWithTriggers(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  max_workers: 4
  agents:
    - name: watcher
      adapter: ollama
      model: llama3
      system: "Watch"
      master: true
      triggers:
        - file.changed
        - task.created
`)
	swarm, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if swarm.Scheduler == nil {
		t.Fatal("expected scheduler for triggered agents")
	}
	if swarm.EventBus == nil {
		t.Fatal("expected event bus")
	}
}

func TestFromBytesWithFSMStates(t *testing.T) {
	yaml := []byte(`
swarm:
  project_id: test
  agents:
    - name: fsm-agent
      adapter: ollama
      model: llama3
      system: "FSM"
      master: true
      states:
        - name: initial
          system: "You are in the initial state"
          options: ["done"]
          routes:
            done: finish
        - name: finish
          system: "Done"
          result: "{{.output}}"
`)
	swarm, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if swarm.RootAgent == nil {
		t.Fatal("expected root agent")
	}
}

func TestFromBytesOpenAIMissingKey(t *testing.T) {
	// Temporarily unset the key
	orig := os.Getenv("OPENAI_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	defer os.Setenv("OPENAI_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: openai
      model: gpt-4
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing OPENAI_API_KEY")
	}
}

func TestFromBytesAnthropicMissingKey(t *testing.T) {
	orig := os.Getenv("ANTHROPIC_API_KEY")
	os.Unsetenv("ANTHROPIC_API_KEY")
	defer os.Setenv("ANTHROPIC_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: anthropic
      model: claude-3
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing ANTHROPIC_API_KEY")
	}
}

func TestFromBytesGeminiMissingKey(t *testing.T) {
	orig := os.Getenv("GEMINI_API_KEY")
	os.Unsetenv("GEMINI_API_KEY")
	defer os.Setenv("GEMINI_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: gemini
      model: gemini-2.5-flash
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing GEMINI_API_KEY")
	}
}

func TestFromBytesDeepseekMissingKey(t *testing.T) {
	orig := os.Getenv("DEEPSEEK_API_KEY")
	os.Unsetenv("DEEPSEEK_API_KEY")
	defer os.Setenv("DEEPSEEK_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: deepseek
      model: ds-chat
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing DEEPSEEK_API_KEY")
	}
}

func TestFromBytesGroqMissingKey(t *testing.T) {
	orig := os.Getenv("GROQ_API_KEY")
	os.Unsetenv("GROQ_API_KEY")
	defer os.Setenv("GROQ_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: groq
      model: llama3
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing GROQ_API_KEY")
	}
}

func TestFromBytesKimiMissingKey(t *testing.T) {
	orig := os.Getenv("KIMI_API_KEY")
	os.Unsetenv("KIMI_API_KEY")
	defer os.Setenv("KIMI_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: kimi
      model: k1
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing KIMI_API_KEY")
	}
}

func TestFromBytesMistralMissingKey(t *testing.T) {
	orig := os.Getenv("MISTRAL_API_KEY")
	os.Unsetenv("MISTRAL_API_KEY")
	defer os.Setenv("MISTRAL_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: mistral
      model: m1
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing MISTRAL_API_KEY")
	}
}

func TestFromBytesOpenRouterMissingKey(t *testing.T) {
	orig := os.Getenv("OPENROUTER_API_KEY")
	os.Unsetenv("OPENROUTER_API_KEY")
	defer os.Setenv("OPENROUTER_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: openrouter
      model: or1
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing OPENROUTER_API_KEY")
	}
}

func TestFromBytesQwenMissingKey(t *testing.T) {
	orig := os.Getenv("QWEN_API_KEY")
	os.Unsetenv("QWEN_API_KEY")
	defer os.Setenv("QWEN_API_KEY", orig)

	yaml := []byte(`
swarm:
  agents:
    - name: a
      adapter: qwen
      model: q1
      system: "test"
      master: true
`)
	_, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err == nil {
		t.Fatal("expected error for missing QWEN_API_KEY")
	}
}

func TestFromYAMLValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swarm.yaml")
	yaml := `
swarm:
  project_id: test-project
  agents:
    - name: coder
      adapter: ollama
      model: llama3
      system: "You are a coder"
      master: true
`
	os.WriteFile(path, []byte(yaml), 0644)

	swarm, err := swarmconfig.FromYAML(context.Background(), path)
	if err != nil {
		t.Fatalf("FromYAML: %v", err)
	}
	if swarm.RootAgent == nil {
		t.Fatal("expected root agent")
	}
	if swarm.Config.Swarm.ProjectID != "test-project" {
		t.Fatalf("expected project_id 'test-project', got %q", swarm.Config.Swarm.ProjectID)
	}
}

func TestExtractOutputKeyParsing(t *testing.T) {
	// This tests the FSM state result key parsing indirectly via a full config
	yaml := []byte(`
swarm:
  agents:
    - name: fsm
      adapter: ollama
      model: llama3
      system: "FSM agent"
      master: true
      states:
        - name: initial
          system: "Decide"
          result: "{{.decision}}"
        - name: implement
          system: "Implement"
          result: "{{.code}}"
          transitions: []
`)
	swarm, err := swarmconfig.FromBytes(context.Background(), yaml)
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}
	if swarm.RootAgent == nil {
		t.Fatal("expected root agent")
	}
}
