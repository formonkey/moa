// Package doctor provides swarm health diagnostics.
//
// It validates a swarm.yaml configuration without actually starting agents,
// checking: YAML syntax, adapter credentials, model reachability, RAG file
// existence, watch paths, MCP availability, and agent graph consistency.
//
// Usage:
//
//	report := doctor.Check(ctx, "./swarm.yaml")
//	report.Print(os.Stdout)
package doctor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/formonkey/moa/swarm/swarmconfig"
)

// Severity levels for diagnostic results.
type Severity int

const (
	OK   Severity = iota
	WARN          // non-fatal but suspicious
	FAIL          // will prevent the swarm from working
)

func (s Severity) String() string {
	switch s {
	case OK:
		return "✅"
	case WARN:
		return "⚠️ "
	case FAIL:
		return "❌"
	}
	return "?"
}

// Result is a single diagnostic check result.
type Result struct {
	Category string
	Check    string
	Severity Severity
	Detail   string
}

// Report is the complete diagnostic output.
type Report struct {
	Results  []Result
	Duration time.Duration
}

// OK returns true if no FAIL results exist.
func (r *Report) OK() bool {
	for _, res := range r.Results {
		if res.Severity == FAIL {
			return false
		}
	}
	return true
}

// Print writes a human-readable report.
func (r *Report) Print(w io.Writer) {
	fmt.Fprintf(w, "\n🩺 go-brain doctor (took %s)\n", r.Duration)
	fmt.Fprintln(w, strings.Repeat("─", 60))

	lastCat := ""
	for _, res := range r.Results {
		if res.Category != lastCat {
			fmt.Fprintf(w, "\n  [%s]\n", res.Category)
			lastCat = res.Category
		}
		fmt.Fprintf(w, "    %s %s", res.Severity, res.Check)
		if res.Detail != "" {
			fmt.Fprintf(w, " — %s", res.Detail)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, strings.Repeat("─", 60))
	var fails, warns, oks int
	for _, res := range r.Results {
		switch res.Severity {
		case FAIL:
			fails++
		case WARN:
			warns++
		case OK:
			oks++
		}
	}
	fmt.Fprintf(w, "  %d passed, %d warnings, %d failures\n\n", oks, warns, fails)

	if r.OK() {
		fmt.Fprintln(w, "  🟢 Swarm is healthy!")
	} else {
		fmt.Fprintln(w, "  🔴 Swarm has issues — fix the failures above.")
	}
	fmt.Fprintln(w)
}

// Check runs all diagnostics against a swarm.yaml file.
func Check(ctx context.Context, filePath string) *Report {
	start := time.Now()
	report := &Report{}

	// 1. File existence
	data, err := os.ReadFile(filePath)
	if err != nil {
		report.add("Config", "File readable", FAIL, err.Error())
		report.Duration = time.Since(start)
		return report
	}
	report.add("Config", "File readable", OK, filePath)

	// 2. YAML syntax
	var cfg swarmconfig.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		report.add("Config", "YAML syntax", FAIL, err.Error())
		report.Duration = time.Since(start)
		return report
	}
	report.add("Config", "YAML syntax", OK, "valid")

	// 3. Swarm structure
	if len(cfg.Swarm.Agents) == 0 {
		report.add("Config", "Agents defined", FAIL, "no agents in swarm config")
	} else {
		report.add("Config", "Agents defined", OK, fmt.Sprintf("%d agents", len(cfg.Swarm.Agents)))
	}

	if cfg.Swarm.Flow != "" {
		report.add("Config", "Flow type", OK, cfg.Swarm.Flow)
	}

	if cfg.Swarm.MaxWorkers > 0 {
		report.add("Config", "Max workers", OK, fmt.Sprintf("%d", cfg.Swarm.MaxWorkers))
	} else {
		report.add("Config", "Max workers", WARN, "not set, defaults to 2")
	}

	// 4. Master agent check
	hasMaster := false
	for _, a := range cfg.Swarm.Agents {
		if a.Master {
			hasMaster = true
			break
		}
	}
	if len(cfg.Swarm.Agents) > 1 && !hasMaster {
		report.add("Agents", "Master agent", FAIL, "multi-agent swarm requires master: true")
	} else if hasMaster {
		report.add("Agents", "Master agent", OK, "defined")
	}

	// 5. Per-agent checks
	agentNames := make(map[string]bool)
	for _, a := range cfg.Swarm.Agents {
		// Duplicate names
		if agentNames[a.Name] {
			report.add("Agents", fmt.Sprintf("Agent '%s'", a.Name), FAIL, "duplicate agent name")
		}
		agentNames[a.Name] = true

		// Adapter + model
		if a.Adapter == "" {
			report.add("Agents", fmt.Sprintf("Agent '%s' adapter", a.Name), FAIL, "missing adapter field")
		} else {
			report.add("Agents", fmt.Sprintf("Agent '%s' adapter", a.Name), OK, fmt.Sprintf("%s/%s", a.Adapter, a.Model))
		}

		// API key check
		checkAdapterKey(report, a.Name, a.Adapter)

		// States
		if len(a.States) > 0 {
			report.add("Agents", fmt.Sprintf("Agent '%s' states", a.Name), OK, fmt.Sprintf("%d states", len(a.States)))
			for _, state := range a.States {
				if state.Name == "" {
					report.add("Agents", fmt.Sprintf("Agent '%s' state", a.Name), FAIL, "state missing name")
				}
			}
		}

		// Triggers
		if len(a.Triggers) > 0 {
			report.add("Agents", fmt.Sprintf("Agent '%s' triggers", a.Name), OK, strings.Join(a.Triggers, ", "))
		}

		// RAG files
		for _, f := range a.RAGs.Files {
			if _, err := os.Stat(f); err != nil {
				report.add("RAG", fmt.Sprintf("Agent '%s' file '%s'", a.Name, f), WARN, "file not found")
			} else {
				report.add("RAG", fmt.Sprintf("Agent '%s' file '%s'", a.Name, f), OK, "exists")
			}
		}

		// Sub-agent references
		for _, subName := range a.Agents {
			found := false
			for _, a2 := range cfg.Swarm.Agents {
				if a2.Name == subName {
					found = true
					break
				}
			}
			if !found {
				report.add("Agents", fmt.Sprintf("Agent '%s' → '%s'", a.Name, subName), FAIL, "references unknown sub-agent")
			}
		}
	}

	// 6. MCP servers
	for name, mcp := range cfg.Swarm.MCPs {
		if mcp.Command == "" {
			report.add("MCP", fmt.Sprintf("Server '%s'", name), FAIL, "missing command")
			continue
		}
		// Check if command exists
		if _, err := exec.LookPath(mcp.Command); err != nil {
			report.add("MCP", fmt.Sprintf("Server '%s' (%s)", name, mcp.Command), WARN,
				fmt.Sprintf("command not found in PATH: %s", mcp.Command))
		} else {
			report.add("MCP", fmt.Sprintf("Server '%s' (%s)", name, mcp.Command), OK, "command available")
		}
	}

	// 7. Adapter reachability
	checkOllamaReachable(ctx, report, cfg)

	// 8. Store config
	if cfg.Swarm.Store.Provider != "" {
		report.add("Store", "Provider", OK, cfg.Swarm.Store.Provider)
		if cfg.Swarm.Store.Path != "" {
			if _, err := os.Stat(cfg.Swarm.Store.Path); err != nil {
				report.add("Store", "Path", WARN, fmt.Sprintf("'%s' does not exist (will be created)", cfg.Swarm.Store.Path))
			} else {
				report.add("Store", "Path", OK, cfg.Swarm.Store.Path)
			}
		}
	}

	report.Duration = time.Since(start)
	return report
}

func (r *Report) add(category, check string, severity Severity, detail string) {
	r.Results = append(r.Results, Result{
		Category: category,
		Check:    check,
		Severity: severity,
		Detail:   detail,
	})
}

// checkAdapterKey verifies the required env var for a given adapter.
func checkAdapterKey(report *Report, agentName, adapter string) {
	envVars := map[string]string{
		"openai":     "OPENAI_API_KEY",
		"anthropic":  "ANTHROPIC_API_KEY",
		"gemini":     "GEMINI_API_KEY",
		"deepseek":   "DEEPSEEK_API_KEY",
		"groq":       "GROQ_API_KEY",
		"kimi":       "KIMI_API_KEY",
		"mistral":    "MISTRAL_API_KEY",
		"openrouter": "OPENROUTER_API_KEY",
		"qwen":       "QWEN_API_KEY",
	}

	envVar, needsKey := envVars[adapter]
	if !needsKey {
		return // ollama doesn't need a key
	}

	if os.Getenv(envVar) == "" {
		report.add("Credentials", fmt.Sprintf("Agent '%s' %s", agentName, envVar), FAIL,
			fmt.Sprintf("environment variable %s not set", envVar))
	} else {
		report.add("Credentials", fmt.Sprintf("Agent '%s' %s", agentName, envVar), OK, "set")
	}
}

// checkOllamaReachable pings Ollama if any agent uses it.
func checkOllamaReachable(ctx context.Context, report *Report, cfg swarmconfig.Config) {
	usesOllama := false
	for _, a := range cfg.Swarm.Agents {
		if a.Adapter == "ollama" {
			usesOllama = true
			break
		}
	}
	if !usesOllama {
		return
	}

	// Try to reach Ollama API
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		report.add("Connectivity", "Ollama server", FAIL,
			"cannot reach http://localhost:11434 — is Ollama running?")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		report.add("Connectivity", "Ollama server", OK, "reachable at localhost:11434")
	} else {
		report.add("Connectivity", "Ollama server", WARN,
			fmt.Sprintf("responded with status %d", resp.StatusCode))
	}
}
