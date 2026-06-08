// swarm-dev-team demonstrates a complete engineering swarm using moa.
//
// This example loads a swarm.yaml that defines 10+ agents with FSM states,
// per-agent RAG, cross-agent transitions, docsearch, triggers, and more.
// Plugins (cost-governor, circuit-breaker) are attached at the runner level.
//
// Usage:
//
//	cd examples/swarm-dev-team
//	go run main.go "Create a task manager with Angular frontend and Go API"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/plugin/circuitbreaker"
	"github.com/formonkey/moa/plugin/costgovernor"
	"github.com/formonkey/moa/plugin/loggingplugin"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/swarmconfig"
	"github.com/formonkey/moa/tool/devtools"
	"github.com/formonkey/moa/tool/gittools"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go \"<your request>\"")
		fmt.Println("Example: go run main.go \"Create a task manager with Angular frontend and Go API\"")
		os.Exit(1)
	}
	prompt := strings.Join(os.Args[1:], " ")
	ctx := context.Background()

	// ── 1. Register tools ───────────────────────────────────────────
	// Tools must be registered BEFORE loading the YAML, because the
	// swarm parser resolves tool names from the configurable registry.
	// devtools.Register() provides: read_file, write_file, edit_file,
	//                               list_dir, search_files, run_command
	// gittools.Register() provides: git_status, git_diff, git_commit,
	//                               git_branch, git_stash, git_log

	if err := devtools.Register("."); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register devtools: %v\n", err)
		os.Exit(1)
	}
	if err := gittools.Register("."); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register gittools: %v\n", err)
		os.Exit(1)
	}

	// ── 2. Load swarm from YAML ──────────────────────────────────────
	// This parses swarm.yaml and builds the full agent tree:
	//   - Resolves all 10 adapters (ollama, openai, gemini, etc.)
	//   - Creates per-agent RAG stores (BadgerDB) and indexes documents
	//   - Builds FSM agents with states, routes, transitions, docsearch
	//   - Wires cross-agent transitions (tech-lead → frontend → backend)
	//   - Sets up trigger-based agents (auditor, tenth-man, ci-fixer, sre)
	//   - Identifies the master agent (tech-lead)

	swarm, err := swarmconfig.FromYAML(ctx, "./swarm.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load swarm: %v\n", err)
		os.Exit(1)
	}

	// ── 3. Configure plugins ─────────────────────────────────────────
	// Plugins wrap the runner and intercept agent lifecycle events.
	// They are NOT defined in swarm.yaml — they're attached here so
	// you have full control over plugin configuration per deployment.

	plugins := []*plugin.Plugin{
		// Structured logging for all agent events
		loggingplugin.New(),

		// Cost Governor: prevent runaway token usage
		costgovernor.New(costgovernor.Config{
			MaxTotalTokens: 500_000,                // hard cap across all agents
			MaxWallTime:    15 * time.Minute,        // kill run after 15 minutes
			ToolCallCaps: map[string]int{
				"run_command": 30,
				"write_file":  50,
			},
		}),

		// Circuit Breaker: detect and break infinite loops
		circuitbreaker.New(circuitbreaker.Config{
			MaxIdenticalCalls:   5,                  // same tool call 5 times = break
			MaxConsecutiveFails: 5,                   // 5 consecutive failures = break
			CooldownDuration:    30 * time.Second,
		}),
	}

	// ── 4. Create runner ─────────────────────────────────────────────

	r, err := runner.New(runner.Config{
		AppName:           "swarm-dev-team",
		Agent:             swarm.RootAgent,
		SessionService:    session.InMemoryService(),
		Plugins:           plugins,
		AutoCreateSession: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create runner: %v\n", err)
		os.Exit(1)
	}
	defer r.Close()

	// ── 5. Start triggered agents (optional) ─────────────────────────
	// The scheduler runs trigger-based agents in the background.
	// In a real app, you'd emit events like "FILE_MODIFIED" or
	// "BUILD_FAILED" to wake these agents up automatically.

	if swarm.Scheduler != nil {
		go swarm.Scheduler.Start(ctx)
		defer swarm.EventBus.Close()
	}

	// ── 6. Run the swarm ─────────────────────────────────────────────

	fmt.Printf("\n🤖 Swarm: %s\n", swarm.RootAgent.Name())
	fmt.Printf("📝 Request: %s\n", prompt)
	fmt.Printf("👥 Agents: %d total\n\n", len(swarm.Agents))

	userMsg := genai.NewContentFromText(prompt, "user")

	for event, err := range r.Run(ctx, "user-1", "session-1", userMsg, agent.RunConfig{}) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n", err)
			break
		}
		if event != nil && event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					fmt.Print(part.Text)
				}
			}
		}
	}

	fmt.Println("\n\n✅ Swarm completed.")
}
