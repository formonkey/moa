package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/swarm/swarmconfig"
	"github.com/formonkey/moa/telemetry"
)

func resolveConfig(args []string) string {
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	config := fs.String("config", "./swarm.yaml", "Path to swarm.yaml")
	_ = fs.Parse(args)
	return *config
}

func loadSwarmAgent(ctx context.Context, configPath string) (agent.Agent, error) {
	swarm, err := swarmconfig.FromYAML(ctx, configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load swarm: %w", err)
	}
	return swarm.RootAgent, nil
}

// cmdRun loads a swarm from YAML and runs it with a single prompt from stdin or args.
func cmdRun(ctx context.Context, args []string) error {
	configPath := resolveConfig(args)

	// Setup telemetry
	shutdown, err := telemetry.Setup(ctx, telemetry.WithServiceName("moa-cli"))
	if err != nil {
		log.Printf("telemetry setup: %v (continuing without telemetry)", err)
	} else {
		defer shutdown(ctx)
	}

	rootAgent, err := loadSwarmAgent(ctx, configPath)
	if err != nil {
		return err
	}

	sessService := session.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:           "moa-cli",
		Agent:             rootAgent,
		SessionService:    sessService,
		AutoCreateSession: true,
	})
	if err != nil {
		return err
	}

	// Read prompt from remaining args or stdin
	prompt := strings.Join(flag.Args(), " ")
	if prompt == "" {
		fmt.Print("Enter prompt: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			prompt = scanner.Text()
		}
	}

	content := genai.NewContentFromText(prompt, "user")
	for event, err := range r.Run(ctx, "cli-user", "cli-session", content, agent.RunConfig{}) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		if event != nil && event.Content != nil {
			for _, p := range event.Content.Parts {
				if p.Text != "" {
					fmt.Printf("[%s] %s\n", event.Author, p.Text)
				}
			}
		}
	}
	return nil
}

// cmdConsole starts an interactive REPL.
func cmdConsole(ctx context.Context, args []string) error {
	configPath := resolveConfig(args)

	rootAgent, err := loadSwarmAgent(ctx, configPath)
	if err != nil {
		return err
	}

	sessService := session.InMemoryService()
	r, err := runner.New(runner.Config{
		AppName:           "moa-console",
		Agent:             rootAgent,
		SessionService:    sessService,
		AutoCreateSession: true,
	})
	if err != nil {
		return err
	}

	fmt.Println("🐝 MOA Console — type 'exit' to quit")
	fmt.Println("─────────────────────────────────────")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return nil
		}

		content := genai.NewContentFromText(input, "user")
		for event, err := range r.Run(ctx, "console-user", "console-session", content, agent.RunConfig{}) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
				continue
			}
			if event != nil && event.Content != nil {
				for _, p := range event.Content.Parts {
					if p.Text != "" {
						fmt.Printf("  [%s] %s\n", event.Author, p.Text)
					}
				}
			}
		}
	}
	return nil
}

// cmdDoctor validates the swarm YAML configuration.
func cmdDoctor(ctx context.Context, args []string) error {
	configPath := resolveConfig(args)

	fmt.Printf("🔍 Validating %s...\n", configPath)

	_, err := loadSwarmAgent(ctx, configPath)
	if err != nil {
		fmt.Printf("❌ Invalid: %v\n", err)
		return err
	}

	fmt.Println("✅ Swarm configuration is valid!")
	return nil
}


