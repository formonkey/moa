// Example: Dev Team via YAML
//
// Loads a 3-agent development team from a single YAML file.
// Tech Lead (Gemini) delegates to Frontend (Qwen) and Backend (Qwen).
//
// Run:
//
//	cd examples/dev-team-yaml
//	export GEMINI_API_KEY=your-key
//	export OPENAI_API_KEY=your-key
//	go run main.go "Add a user profile page with a REST API endpoint"
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/guardrail"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/rag"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/security"
	"github.com/formonkey/moa/session"
)

func main() {
	ctx := context.Background()

	// 1. Load the entire agent tree from ONE yaml file
	techLead, err := configurable.FromConfig(ctx, "./dev-team.yaml")
	fatal("load agents", err)

	// 2. Load plugins defined in the YAML
	plugins, err := configurable.PluginsFromConfig(ctx, "./dev-team.yaml")
	fatal("load plugins", err)

	// 3. RAG — index project docs so agents can search architecture guidelines
	ragStore, err := rag.NewStore("./.moa_rag")
	fatal("create RAG", err)
	defer ragStore.Close()
	ragStore.IndexFile("../../docs/developer-agent.md")
	ragStore.IndexFile("../../docs/security.md")

	// 4. Security + Guardrails (added programmatically on top of YAML plugins)
	allPlugins := append(plugins,
		security.NewPlugin(security.Standard("./")),
		guardrail.NewPlugin(
			guardrail.PromptInjection(),
			guardrail.PII(guardrail.PIIConfig{
				Block: []guardrail.PIIType{guardrail.PIICreditCard, guardrail.PIISSN},
			}),
		),
	)

	// 5. Run
	r, err := runner.New(runner.Config{
		AppName:           "dev-team",
		Agent:             techLead,
		Plugins:           allPlugins,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	fatal("create runner", err)
	defer r.Close()

	userInput := "Add a user profile page with a REST API endpoint"
	if len(os.Args) > 1 {
		userInput = strings.Join(os.Args[1:], " ")
	}

	fmt.Printf("🚀 Dev Team (YAML mode)\n")
	fmt.Printf("📋 Task: %s\n\n", userInput)

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

// Ensure plugin import is used.
var _ *plugin.Plugin
