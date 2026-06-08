// Package fsmagent provides a stateful FSM agent that supports conditional routing,
// cross-agent transitions, and template interpolation for declarative YAML swarms.
//
// Unlike sequentialagent, fsmagent understands:
//   - options/routes: parse LLM response and jump to the matched state
//   - transitions: execute a list of states (including cross-agent like "frontend.initial")
//   - template interpolation: {{.varname}} resolved from session state
package fsmagent

import (
	"fmt"
	"iter"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/agent/llmagent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

// DocSearchConfig configures documentation search via a librarian agent.
type DocSearchConfig struct {
	AgentName  string // name of the librarian agent in AgentMap
	Prompt     string // custom search prompt (optional)
	ContextVar string // variable name to store results (e.g. "frontend_rules")
	Required   bool   // if true, fail if docsearch returns nothing
}

// StateConfig defines a single FSM state.
type StateConfig struct {
	Name          string
	System        string
	Prompt        string
	Options       []string
	Routes        map[string]string // option -> target state name
	Transitions   []string          // e.g. ["frontend-angular.initial", "reviewer.initial", "finish"]
	ResultKey     string            // output key to save result (e.g. "result", "architecture")
	DocSearch     *DocSearchConfig  // optional docsearch before this state
	Tools         []tool.Tool       // per-state tools (only active during this state)
	MaxRetries    int               // max times this state can be re-entered (0 = unlimited)
	FallbackState string            // state to go to when max_retries exceeded
}

// Config configures the FSM agent.
type Config struct {
	Name        string
	Description string
	Model       model.LLM
	States      []StateConfig
	AgentMap    map[string]agent.Agent // full swarm agent map for cross-agent resolution
	RAGFiles    []string               // paths to RAG documentation files
}

// New creates an FSM agent with conditional routing and cross-agent transitions.
func New(cfg Config) (agent.Agent, error) {
	if len(cfg.States) == 0 {
		return nil, fmt.Errorf("fsmagent: at least one state is required")
	}

	stateIndex := make(map[string]*StateConfig)
	for i := range cfg.States {
		stateIndex[cfg.States[i].Name] = &cfg.States[i]
	}

	if _, ok := stateIndex["initial"]; !ok {
		return nil, fmt.Errorf("fsmagent: agent %q must have an 'initial' state", cfg.Name)
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				runFSM(ctx, cfg, stateIndex, "initial", true, yield)
			}
		},
	})
}

// runFSM is the core FSM execution loop.
// startState is typically "initial" but can be any state name when re-entering
// the FSM from a transition.
func runFSM(
	ctx agent.InvocationContext,
	cfg Config,
	stateIndex map[string]*StateConfig,
	startState string,
	seedState bool,
	yield func(*session.Event, error) bool,
) {
	if seedState {
		// --- Inject {{.prompt}} and {{.history}} into session state ---
		seedSessionState(ctx)
		// --- Load RAG files into session state ---
		loadRAGFiles(ctx, cfg)
	}

	currentState := startState
	bus := ctx.EventBus()
	stateVisits := make(map[string]int) // track how many times each state is entered

	for currentState != "" {
		if ctx.Ended() {
			return
		}

		state, ok := stateIndex[currentState]
		if !ok {
			// Try resolving as cross-agent reference (agent.state)
			if !executeCrossAgent(ctx, cfg, currentState, yield) {
				yield(nil, fmt.Errorf("fsmagent: unknown state %q in agent %q", currentState, cfg.Name))
			}
			return
		}

		bus.Emit(agent.BusEvent{
			Type:    agent.EventAgentSwitch,
			Agent:   cfg.Name,
			Payload: fmt.Sprintf("state:%s", state.Name),
		})

		// --- Check MaxRetries ---
		stateVisits[currentState]++
		if state.MaxRetries > 0 && stateVisits[currentState] > state.MaxRetries {
			bus.Emit(agent.BusEvent{
				Type:    agent.EventAgentSwitch,
				Agent:   cfg.Name,
				Payload: fmt.Sprintf("max_retries_exceeded:%s (limit=%d)", state.Name, state.MaxRetries),
			})
			if state.FallbackState != "" {
				currentState = state.FallbackState
				continue
			}
			yield(nil, fmt.Errorf("fsmagent: state %q exceeded max_retries (%d) with no fallback", state.Name, state.MaxRetries))
			return
		}

		// --- Check if this state is a transition pipeline ---
		if len(state.Transitions) > 0 && state.System == "Routing pipeline..." {
			for _, target := range state.Transitions {
				if ctx.Ended() {
					return
				}
				executeTarget(ctx, cfg, stateIndex, target, yield)
			}
			return
		}

		// --- Execute DocSearch if configured ---
		if state.DocSearch != nil {
			executeDocSearch(ctx, cfg, state.DocSearch, yield)
		}

		// --- Build instruction with template interpolation ---
		instruction := interpolateTemplate(state.System, ctx.Session())
		prompt := interpolateTemplate(state.Prompt, ctx.Session())
		if prompt != "" {
			instruction += "\n\n" + prompt
		}
		if len(state.Options) > 0 {
			instruction += "\n\nYou MUST respond with EXACTLY one of: " + strings.Join(state.Options, ", ")
		}

		// --- Execute the LLM for this state ---
		stateName := fmt.Sprintf("%s.%s", cfg.Name, state.Name)
		stateAgent, err := llmagent.New(llmagent.Config{
			Name:        stateName,
			Description: fmt.Sprintf("State '%s' of agent '%s'", state.Name, cfg.Name),
			Model:       cfg.Model,
			Instruction: instruction,
			Tools:       state.Tools,
			OutputKey:   state.ResultKey,
		})
		if err != nil {
			yield(nil, fmt.Errorf("fsmagent: failed to build state agent %q: %w", stateName, err))
			return
		}

		childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
			Ctx:          ctx,
			Agent:        stateAgent,
			Artifacts:    ctx.Artifacts(),
			Memory:       ctx.Memory(),
			Session:      ctx.Session(),
			InvocationID: ctx.InvocationID(),
			Branch:       ctx.Branch() + "." + stateName,
			UserContent:  ctx.UserContent(),
			RunConfig:    ctx.RunConfig(),
			ParentMap:    ctx.ParentMap(),
			EventBus:     ctx.EventBus(),
		})

		var lastText string
		for event, err := range stateAgent.Run(childCtx) {
			if err != nil {
				yield(nil, err)
				return
			}
			if event != nil && event.Content != nil {
				for _, p := range event.Content.Parts {
					if p.Text != "" {
						lastText = p.Text
					}
				}
			}
			if !yield(event, nil) {
				return
			}
		}

		// --- Store result in session state for interpolation ---
		if state.ResultKey != "" && lastText != "" {
			if err := ctx.Session().State().Set(
				fmt.Sprintf("%s.%s", cfg.Name, state.ResultKey),
				lastText,
			); err != nil {
				log.Printf("[WARN] fsmagent: failed to set state %q: %v", state.ResultKey, err)
			}
			if err := ctx.Session().State().Set(state.ResultKey, lastText); err != nil {
				log.Printf("[WARN] fsmagent: failed to set state %q: %v", state.ResultKey, err)
			}
		}

		// --- Route dispatch ---
		if len(state.Routes) > 0 && lastText != "" {
			response := strings.TrimSpace(lastText)
			nextState, ok := state.Routes[response]
			if ok {
				currentState = nextState
				continue
			}
			// Fuzzy match: check if response contains any option
			matched := false
			for option, target := range state.Routes {
				if strings.Contains(strings.ToUpper(response), strings.ToUpper(option)) {
					currentState = target
					matched = true
					break
				}
			}
			if matched {
				continue
			}
			// No match — terminal
			return
		}

		// --- Sequential transitions ---
		if len(state.Transitions) > 0 {
			for _, target := range state.Transitions {
				if ctx.Ended() {
					return
				}
				executeTarget(ctx, cfg, stateIndex, target, yield)
			}
			return
		}

		// --- Terminal state (has result, no routes, no transitions) ---
		return
	}
}

// executeTarget handles a transition target, which can be:
//   - A local state name (e.g. "implement")
//   - A cross-agent reference (e.g. "frontend-angular.initial")
func executeTarget(
	ctx agent.InvocationContext,
	cfg Config,
	stateIndex map[string]*StateConfig,
	target string,
	yield func(*session.Event, error) bool,
) {
	// Check if it's a local state
	if _, ok := stateIndex[target]; ok {
		// Re-enter the FSM at this state (no seed — already done)
		runFSM(ctx, cfg, stateIndex, target, false, yield)
		return
	}
	// Cross-agent reference
	executeCrossAgent(ctx, cfg, target, yield)
}



// executeCrossAgent resolves "agent-name.state-name" and runs that agent.
func executeCrossAgent(
	ctx agent.InvocationContext,
	cfg Config,
	target string,
	yield func(*session.Event, error) bool,
) bool {
	parts := strings.SplitN(target, ".", 2)
	if len(parts) != 2 {
		return false
	}

	agentName := parts[0]
	targetAgent, ok := cfg.AgentMap[agentName]
	if !ok {
		yield(nil, fmt.Errorf("fsmagent: cross-agent transition references unknown agent %q", agentName))
		return true
	}

	bus := ctx.EventBus()
	bus.Emit(agent.BusEvent{
		Type:    agent.EventAgentSwitch,
		Agent:   cfg.Name,
		Payload: fmt.Sprintf("cross:%s", target),
	})

	childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:          ctx,
		Agent:        targetAgent,
		Artifacts:    ctx.Artifacts(),
		Memory:       ctx.Memory(),
		Session:      ctx.Session(),
		InvocationID: ctx.InvocationID(),
		Branch:       ctx.Branch() + "." + agentName,
		UserContent:  ctx.UserContent(),
		RunConfig:    ctx.RunConfig(),
		ParentMap:    ctx.ParentMap(),
		EventBus:     ctx.EventBus(),
	})

	for event, err := range targetAgent.Run(childCtx) {
		if !yield(event, err) {
			return true
		}
	}
	return true
}

// interpolateTemplate replaces {{.varname}} and {{.agent.varname}} with session state values.
var tmplPattern = regexp.MustCompile(`\{\{\.([a-zA-Z0-9_\-\.]+)\}\}`)

func interpolateTemplate(tmpl string, sess session.Session) string {
	if !strings.Contains(tmpl, "{{.") {
		return tmpl
	}
	return tmplPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := strings.TrimPrefix(match, "{{.")
		key = strings.TrimSuffix(key, "}}")
		val, err := sess.State().Get(key)
		if err != nil || val == nil {
			return match // leave unresolved
		}
		if s, ok := val.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", val)
	})
}

// seedSessionState extracts the user's prompt from UserContent and builds
// a chat history string from session events, injecting both into session
// state so {{.prompt}} and {{.history}} templates resolve correctly.
func seedSessionState(ctx agent.InvocationContext) {
	sess := ctx.Session()

	// Extract user prompt text from UserContent
	if ctx.UserContent() != nil {
		var parts []string
		for _, p := range ctx.UserContent().Parts {
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		}
		if len(parts) > 0 {
			if err := sess.State().Set("prompt", strings.Join(parts, "\n")); err != nil {
				log.Printf("[WARN] fsmagent: failed to set 'prompt' state: %v", err)
			}
		}
	}

	// Build history from session events
	events := sess.Events()
	if events.Len() > 0 {
		var history strings.Builder
		// Take last N events to keep history manageable
		start := 0
		if events.Len() > 20 {
			start = events.Len() - 20
		}
		for i := start; i < events.Len(); i++ {
			event := events.At(i)
			if event.Content == nil {
				continue
			}
			role := event.Author
			if role == "" {
				role = "assistant"
			}
			for _, p := range event.Content.Parts {
				if p.Text != "" {
					history.WriteString(fmt.Sprintf("[%s]: %s\n", role, p.Text))
				}
			}
		}
		if history.Len() > 0 {
			if err := sess.State().Set("history", history.String()); err != nil {
				log.Printf("[WARN] fsmagent: failed to set 'history' state: %v", err)
			}
		}
	}
}

// loadRAGFiles reads the configured RAG documentation files and stores their
// concatenated content in session state as "rag_context". This makes the
// documentation available for template interpolation in prompts.
func loadRAGFiles(ctx agent.InvocationContext, cfg Config) {
	if len(cfg.RAGFiles) == 0 {
		return
	}

	var ragContent strings.Builder
	for _, path := range cfg.RAGFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			ragContent.WriteString(fmt.Sprintf("\n--- [ERROR: could not read %s: %v] ---\n", path, err))
			continue
		}
		ragContent.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n%s\n", path, string(data)))
	}

	if ragContent.Len() > 0 {
		if err := ctx.Session().State().Set("rag_context", ragContent.String()); err != nil {
			log.Printf("[WARN] fsmagent: failed to set 'rag_context' state: %v", err)
		}
	}
}

// executeDocSearch delegates a search query to a librarian agent (or any docsearch
// agent) and stores the result in session state under the configured context_var.
// This enables states like audit_frontend to receive injected knowledge automatically.
func executeDocSearch(
	ctx agent.InvocationContext,
	cfg Config,
	ds *DocSearchConfig,
	yield func(*session.Event, error) bool,
) {
	if ds.AgentName == "" {
		return
	}

	librarianAgent, ok := cfg.AgentMap[ds.AgentName]
	if !ok {
		if ds.Required {
			yield(nil, fmt.Errorf("fsmagent: docsearch references unknown agent %q", ds.AgentName))
		}
		return
	}

	// Build the docsearch prompt
	searchPrompt := ds.Prompt
	if searchPrompt == "" {
		// Use the current user prompt as the search query
		searchPrompt = "Search for relevant documentation"
		if val, err := ctx.Session().State().Get("prompt"); err == nil {
			if s, ok := val.(string); ok {
				searchPrompt = s
			}
		}
	}
	searchPrompt = interpolateTemplate(searchPrompt, ctx.Session())

	// Execute the librarian agent
	childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:          ctx,
		Agent:        librarianAgent,
		Artifacts:    ctx.Artifacts(),
		Memory:       ctx.Memory(),
		Session:      ctx.Session(),
		InvocationID: ctx.InvocationID(),
		Branch:       ctx.Branch() + ".docsearch." + ds.AgentName,
		UserContent:  ctx.UserContent(),
		RunConfig:    ctx.RunConfig(),
		ParentMap:    ctx.ParentMap(),
		EventBus:     ctx.EventBus(),
	})

	// Inject the search prompt so the librarian can use it
	if err := ctx.Session().State().Set("docsearch_query", searchPrompt); err != nil {
		log.Printf("[WARN] fsmagent: failed to set 'docsearch_query' state: %v", err)
	}

	var resultText string
	for event, err := range librarianAgent.Run(childCtx) {
		if err != nil {
			if ds.Required {
				yield(nil, fmt.Errorf("fsmagent: docsearch agent %q failed: %w", ds.AgentName, err))
			}
			return
		}
		if event != nil && event.Content != nil {
			for _, p := range event.Content.Parts {
				if p.Text != "" {
					resultText += p.Text
				}
			}
		}
	}

	// Store the docsearch result in session state
	contextVar := ds.ContextVar
	if contextVar == "" {
		contextVar = "docsearch_result"
	}
	if resultText != "" {
		if err := ctx.Session().State().Set(contextVar, resultText); err != nil {
			log.Printf("[WARN] fsmagent: failed to set %q state: %v", contextVar, err)
		}
	} else if ds.Required {
		yield(nil, fmt.Errorf("fsmagent: docsearch agent %q returned empty result", ds.AgentName))
	}
}


