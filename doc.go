// Package moa (Multi-Agent Orchestrator) is a production-grade framework for
// building, orchestrating, and deploying multi-agent AI systems in Go.
//
// # Features
//
//   - 6 agent types: LLM, Sequential, Parallel, Loop, Competitive, Debate
//   - 10 model providers: Gemini, OpenAI, Anthropic, Ollama, DeepSeek, Groq, Mistral, OpenRouter, Qwen, Kimi
//   - Plugin system: Cost Governor, Circuit Breaker, Teacher Escalation, Guardrails, OpenTelemetry
//   - YAML or Go configuration with inline sub-agents
//   - RAG (BadgerDB vector store), Memory, Skills, Skill Optimizer (SkillOpt)
//   - Swarm coordination: Blackboard, Scheduler, FSM agents, Webhooks, Cron
//   - A2A protocol support with health checks
//   - SSE streaming, structured logging (slog), retry with backoff
//   - Eval testing framework for agent quality
//
// # Quick Start
//
//	import "github.com/formonkey/moa/agent/llmagent"
//	import "github.com/formonkey/moa/model/ollama"
//	import "github.com/formonkey/moa/runner"
//
//	model := ollama.NewClient("", "qwen3:8b")
//	agent, _ := llmagent.New(llmagent.Config{
//	    Name:        "assistant",
//	    Model:       model,
//	    Instruction: "You are a helpful assistant.",
//	})
//	r, _ := runner.New(runner.Config{Agent: agent, ...})
//
// See https://github.com/formonkey/moa for full documentation and examples.
package moa
