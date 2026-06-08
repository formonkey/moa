// Command moa provides the CLI for the MOA (Multi-Agent Orchestration) framework.
//
// Install:
//
//	go install github.com/formonkey/moa/cmd/moa@latest
//
// Usage:
//
//	moa run                          # Run the swarm from swarm.yaml
//	moa run --config ./my-swarm.yaml # Run with a custom config
//	moa console                      # Interactive REPL for testing
//	moa doctor                       # Validate swarm configuration
//	moa eval ./tests.yaml            # Run evaluation test suite
//	moa serve                        # Start HTTP server with SSE
//	moa version                      # Print version
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := strings.ToLower(os.Args[1])
	var err error

	switch cmd {
	case "run":
		err = cmdRun(ctx, os.Args[2:])
	case "console":
		err = cmdConsole(ctx, os.Args[2:])
	case "doctor":
		err = cmdDoctor(ctx, os.Args[2:])
	case "version":
		fmt.Printf("moa %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`moa - Multi-Agent Orchestration CLI

Usage:
  moa <command> [flags]

Commands:
  run        Run a swarm from YAML config
  console    Interactive REPL for testing agents
  doctor     Validate swarm configuration
  version    Print version

Flags:
  --config <path>   Path to swarm.yaml (default: ./swarm.yaml)

Install:
  go install github.com/formonkey/moa/cmd/moa@latest`)
}
