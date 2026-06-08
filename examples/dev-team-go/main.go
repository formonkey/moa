// Example: Dev Team via pure Go
//
// Same 3-agent team as dev-team-yaml, but 100% Go code — no YAML files.
// Tech Lead (Gemini) delegates to Frontend (Qwen) and Backend (Qwen).
//
// Run:
//
//	cd examples/dev-team-go
//	export GEMINI_API_KEY=your-key
//	export OPENAI_API_KEY=your-key
//	go run main.go "Add a user profile page with a REST API endpoint"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/llmagent"
	"github.com/formonkey/moa/guardrail"
	"github.com/formonkey/moa/model/gemini"
	"github.com/formonkey/moa/model/ollama"
	"github.com/formonkey/moa/model/openai"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/plugin/circuitbreaker"
	"github.com/formonkey/moa/plugin/costgovernor"
	"github.com/formonkey/moa/plugin/teacherescalation"
	"github.com/formonkey/moa/rag"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/security"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/skill"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/codegraph"
	"github.com/formonkey/moa/tool/devtools"
	"github.com/formonkey/moa/tool/scraper"
)

func main() {
	ctx := context.Background()

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 1. SKILLS — reusable capability bundles
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	goSkill := &skill.SimpleSkill{
		SkillName:        "go_best_practices",
		SkillDescription: "Go coding standards and patterns",
		Directive: `When writing Go code:
- Use error wrapping with fmt.Errorf("context: %w", err)
- Prefer interfaces over concrete types in function signatures
- Use table-driven tests with t.Run
- Always handle errors, never use _ for errors
- Use context.Context as the first parameter`,
	}

	reactSkill := &skill.SimpleSkill{
		SkillName:        "react_best_practices",
		SkillDescription: "React/TypeScript coding standards",
		Directive: `When writing React code:
- Use functional components with hooks
- Define Props interfaces for every component
- Implement error boundaries for each page
- Use loading/error/success states for async operations`,
	}

	skillRegistry := skill.NewRegistry()
	skillRegistry.Register(goSkill)
	skillRegistry.Register(reactSkill)

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 2. MODELS — 3 different providers
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	localModel := ollama.NewClient("", "qwen3:8b") // Ollama on localhost
	geminiModel, err := gemini.NewClient(ctx, os.Getenv("GEMINI_API_KEY"), "gemini-2.5-flash")
	fatal("create gemini client", err)
	teacherModel := openai.NewClient(openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
		Model:  "gpt-4o",
	})

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 3. RAG — documentation search
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	ragStore, err := rag.NewStore("./.moa_rag")
	fatal("create RAG", err)
	defer ragStore.Close()
	ragStore.IndexFile("../../docs/developer-agent.md")
	ragStore.IndexFile("../../docs/security.md")

	ragTool, err := rag.NewSearchDocsTool(ragStore)
	fatal("create RAG tool", err)

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 4. TOOLS — devtools, codegraph, scraper
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	frontendTools, err := devtools.NewToolset("./frontend")
	fatal("create frontend devtools", err)

	backendTools, err := devtools.NewToolset("./backend")
	fatal("create backend devtools", err)

	backendCodegraph, err := codegraph.NewToolset(codegraph.Config{ProjectDir: "./backend"})
	fatal("create backend codegraph", err)

	scrapeTool := scraper.NewScrapeTool(scraper.Config{Timeout: 15 * time.Second})

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 5. AGENTS — the dev team
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━

	// Frontend Developer
	frontendDev, err := llmagent.New(llmagent.Config{
		Name:  "frontend-dev",
		Model: localModel,
		Instruction: `You are a Senior Frontend Developer (React + TypeScript).
Read existing code FIRST. Write typed components with Props interfaces.
When done, transfer back to 'tech-lead' with a summary.`,
		Tools:                   frontendTools,
		Toolsets:                []tool.Toolset{reactSkill},
		OutputKey:               "frontend_changes",
		DisallowTransferToPeers: true,
	})
	fatal("create frontend-dev", err)

	// Backend Developer
	backendDev, err := llmagent.New(llmagent.Config{
		Name:  "backend-dev",
		Model: localModel,
		Instruction: `You are a Senior Backend Developer (Go).
Write idiomatic Go with proper error handling and table-driven tests.
Run 'go test' before reporting back. Transfer to 'tech-lead' when done.`,
		Tools:                   append(backendTools, scrapeTool),
		Toolsets:                []tool.Toolset{backendCodegraph, goSkill},
		OutputKey:               "backend_changes",
		DisallowTransferToPeers: true,
	})
	fatal("create backend-dev", err)

	// Tech Lead — orchestrator
	techLead, err := llmagent.New(llmagent.Config{
		Name:  "tech-lead",
		Model: geminiModel,
		Instruction: `You are a Senior Tech Lead. Your team:
- 'frontend-dev': React/TypeScript specialist
- 'backend-dev': Go API specialist
Analyze requests, delegate to the right specialist, review their work.
Use search_docs to check architecture guidelines first.`,
		Tools:     []tool.Tool{ragTool, scrapeTool},
		SubAgents: []agent.Agent{frontendDev, backendDev},
		OutputKey: "tech_lead_summary",
	})
	fatal("create tech-lead", err)

	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	// 6. PLUGINS — protection + observability
	// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
	allPlugins := []*plugin.Plugin{
		costgovernor.New(costgovernor.Config{
			MaxTotalTokens: 200_000,
			MaxWallTime:    10 * time.Minute,
			ToolCallCaps:   map[string]int{"run_command": 20, "write_file": 30},
		}),
		circuitbreaker.New(circuitbreaker.Config{
			MaxIdenticalCalls:   5,
			MaxConsecutiveFails: 5,
			CooldownDuration:   30 * time.Second,
		}),
		teacherescalation.New(teacherescalation.Config{
			TeacherModel:  teacherModel,
			SkillRegistry: skillRegistry,
			MinConfidence: 0.7,
		}),
		security.NewPlugin(security.Standard("./")),
		guardrail.NewPlugin(
			guardrail.PromptInjection(),
			guardrail.PII(guardrail.PIIConfig{
				Block:    []guardrail.PIIType{guardrail.PIICreditCard, guardrail.PIISSN},
				Sanitize: []guardrail.PIIType{guardrail.PIIEmail, guardrail.PIIPhone},
			}),
			guardrail.ContentPolicy(guardrail.ContentConfig{
				BlockPatterns:  []string{`DROP TABLE`, `DELETE FROM`, `rm -rf /`},
				MaxOutputLength: 50000,
			}),
		),
	}

	// ━━━━━━━━━━━━━
	// 7. RUN
	// ━━━━━━━━━━━━━
	r, err := runner.New(runner.Config{
		AppName: "dev-team", Agent: techLead, Plugins: allPlugins,
		SessionService: session.InMemoryService(), AutoCreateSession: true,
	})
	fatal("create runner", err)
	defer r.Close()

	userInput := "Add a user profile page with a REST API endpoint"
	if len(os.Args) > 1 {
		userInput = strings.Join(os.Args[1:], " ")
	}

	fmt.Printf("🚀 Dev Team (Go mode)\n📋 Task: %s\n\n", userInput)
	msg := genai.NewContentFromText(userInput, "user")
	for event, err := range r.Run(ctx, "dev", "s1", msg, agent.RunConfig{}) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n", err)
			break
		}
		if event != nil && event.Content != nil {
			for _, p := range event.Content.Parts {
				if p.Text != "" {
					fmt.Print(p.Text)
				}
			}
		}
	}
	fmt.Println("\n✅ Done!")
}

func fatal(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to %s: %v\n", what, err)
		os.Exit(1)
	}
}
