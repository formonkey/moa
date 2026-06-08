// Package llmagent provides an LLM-powered agent implementation for go-brain.
//
// An LLM agent wraps a language model with tools, instructions, callbacks,
// and a tool execution loop. It handles the full generate→function_call→execute→respond
// cycle automatically, yielding session events as an iterator.
package llmagent

import (
	"fmt"
	"iter"
	"strings"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

// New creates a new LLM-powered agent.
func New(cfg Config) (agent.Agent, error) {
	a := &llmAgent{
		model:                      cfg.Model,
		instruction:                cfg.Instruction,
		instructionProvider:        cfg.InstructionProvider,
		globalInstruction:          cfg.GlobalInstruction,
		globalInstructionProvider:  cfg.GlobalInstructionProvider,
		inputSchema:           cfg.InputSchema,
		outputSchema:          cfg.OutputSchema,
		outputKey:             cfg.OutputKey,
		includeContents:       cfg.IncludeContents,
		tools:                 cfg.Tools,
		toolsets:              cfg.Toolsets,
		generateContentConfig: cfg.GenerateContentConfig,

		beforeModelCallbacks:  cfg.BeforeModelCallbacks,
		afterModelCallbacks:   cfg.AfterModelCallbacks,
		onModelErrorCallbacks: cfg.OnModelErrorCallbacks,
		beforeToolCallbacks:   cfg.BeforeToolCallbacks,
		afterToolCallbacks:    cfg.AfterToolCallbacks,
		onToolErrorCallbacks:  cfg.OnToolErrorCallbacks,

		disallowTransferToParent: cfg.DisallowTransferToParent,
		disallowTransferToPeers:  cfg.DisallowTransferToPeers,
	}


	baseAgent, err := agent.New(agent.Config{
		Name:                 cfg.Name,
		Description:          cfg.Description,
		SubAgents:            cfg.SubAgents,
		BeforeAgentCallbacks: cfg.BeforeAgentCallbacks,
		Run:                  a.run,
		AfterAgentCallbacks:  cfg.AfterAgentCallbacks,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create llm agent: %w", err)
	}

	a.Agent = baseAgent
	return a, nil
}

// Config holds the full configuration for an LLM agent.
type Config struct {
	Name        string
	Description string
	SubAgents   []agent.Agent

	BeforeAgentCallbacks []agent.BeforeAgentCallback
	AfterAgentCallbacks  []agent.AfterAgentCallback

	// Model used by the agent.
	Model model.LLM
	// GenerateContentConfig for additional generation settings (temperature, safety, etc.).
	GenerateContentConfig *genai.GenerateContentConfig

	// Instruction guides the agent's behavior. Supports {key} placeholders resolved from state.
	Instruction string
	// InstructionProvider creates instructions dynamically. Takes precedence over Instruction.
	InstructionProvider InstructionProvider
	// GlobalInstruction is shared across all agents in the tree. Only root agent's takes effect.
	GlobalInstruction string
	// GlobalInstructionProvider creates global instructions dynamically.
	// Takes precedence over GlobalInstruction if both are set.
	GlobalInstructionProvider InstructionProvider

	// Tools available to the agent.
	Tools []tool.Tool
	// Toolsets provide dynamic tool collections.
	Toolsets []tool.Toolset

	// InputSchema when agent is used as a tool.
	InputSchema *genai.Schema
	// OutputSchema constrains agent replies. When set, agent can only reply (no tools).
	OutputSchema *genai.Schema
	// OutputKey saves output to session state under this key.
	OutputKey string

	// IncludeContents controls conversation history inclusion.
	IncludeContents IncludeContents

	DisallowTransferToParent bool
	DisallowTransferToPeers  bool

	// RAGFiles are paths to documentation files declared in swarm.yaml.
	// They are NOT loaded into the prompt. Use a search_docs tool instead.
	RAGFiles []string

	BeforeModelCallbacks  []BeforeModelCallback
	AfterModelCallbacks   []AfterModelCallback
	OnModelErrorCallbacks []OnModelErrorCallback
	BeforeToolCallbacks   []BeforeToolCallback
	AfterToolCallbacks    []AfterToolCallback
	OnToolErrorCallbacks  []OnToolErrorCallback
}

// Callback types

// BeforeModelCallback is called before sending a request to the model.
// Return non-nil LLMResponse to skip the actual model call (e.g. for caching).
type BeforeModelCallback func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error)

// AfterModelCallback is called after receiving a response from the model.
type AfterModelCallback func(ctx agent.CallbackContext, resp *model.LLMResponse, err error) (*model.LLMResponse, error)

// OnModelErrorCallback is called when the model returns an error.
type OnModelErrorCallback func(ctx agent.CallbackContext, req *model.LLMRequest, err error) (*model.LLMResponse, error)

// BeforeToolCallback is called before a tool's execution.
type BeforeToolCallback func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error)

// AfterToolCallback is called after a tool's execution.
type AfterToolCallback func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any, err error) (map[string]any, error)

// OnToolErrorCallback is called when a tool execution fails.
type OnToolErrorCallback func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error)

// InstructionProvider creates instructions dynamically per invocation.
type InstructionProvider func(ctx agent.ReadonlyContext) (string, error)

// PluginHooksProvider is the interface that plugin.Manager satisfies.
// It allows llmagent to call runner-level plugin callbacks during execution.
// The runner passes this via InvocationContext.PluginHooks().
type PluginHooksProvider interface {
	BeforeModel(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error)
	AfterModel(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error)
	OnModelError(ctx agent.CallbackContext, req *model.LLMRequest, origErr error) (*model.LLMResponse, error)
	BeforeTool(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error)
	AfterTool(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error)
	OnToolError(ctx agent.CallbackContext, t tool.Tool, args map[string]any, origErr error) (map[string]any, error)
}

// pluginHooks extracts the PluginHooksProvider from the invocation context, if any.
func pluginHooks(ctx agent.InvocationContext) PluginHooksProvider {
	h := ctx.PluginHooks()
	if h == nil {
		return nil
	}
	if ph, ok := h.(PluginHooksProvider); ok {
		return ph
	}
	return nil
}

// IncludeContents controls what conversation history the agent receives.
type IncludeContents string

const (
	IncludeContentsNone    IncludeContents = "none"
	IncludeContentsDefault IncludeContents = "default"
)

// --- llmAgent implementation ---

type llmAgent struct {
	agent.Agent // embedded base agent

	model                 model.LLM
	instruction                string
	instructionProvider        InstructionProvider
	globalInstruction          string
	globalInstructionProvider  InstructionProvider
	inputSchema           *genai.Schema
	outputSchema          *genai.Schema
	outputKey             string
	includeContents       IncludeContents
	tools                 []tool.Tool
	toolsets              []tool.Toolset
	generateContentConfig *genai.GenerateContentConfig


	beforeModelCallbacks  []BeforeModelCallback
	afterModelCallbacks   []AfterModelCallback
	onModelErrorCallbacks []OnModelErrorCallback
	beforeToolCallbacks   []BeforeToolCallback
	afterToolCallbacks    []AfterToolCallback
	onToolErrorCallbacks  []OnToolErrorCallback

	disallowTransferToParent bool
	disallowTransferToPeers  bool
}

func (a *llmAgent) FindAgent(name string) agent.Agent {
	if a.Name() == name {
		return a
	}
	return a.Agent.FindSubAgent(name)
}

// DisallowTransferToParent implements agent.LLMAgentInternal.
func (a *llmAgent) DisallowTransferToParent() bool {
	return a.disallowTransferToParent
}

var _ agent.LLMAgentInternal = (*llmAgent)(nil)

// run is the core LLM agent loop: build request → call model → handle tool calls → repeat
func (a *llmAgent) run(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		bus := ctx.EventBus()
		bus.Emit(agent.BusEvent{Type: agent.EventRunStarted, Agent: a.Name()})

		// Build the LLM request ONCE before the loop.
		// Tool call/response contents are appended to req.Contents across iterations
		// so the model sees the full conversation history.
		req, err := a.buildRequest(ctx)
		if err != nil {
			yield(nil, fmt.Errorf("failed to build request: %w", err))
			return
		}

		for {
			if ctx.Ended() {
				return
			}

			// Re-pack tools each iteration. This allows lazy toolsets
			// (e.g., toolbox) to dynamically activate tools mid-conversation.
			if err := a.packTools(ctx, req); err != nil {
				yield(nil, fmt.Errorf("failed to pack tools: %w", err))
				return
			}

			// Run before-model callbacks (plugin hooks first, then agent-level)
			var cachedResp *model.LLMResponse
			if ph := pluginHooks(ctx); ph != nil {
				resp, err := ph.BeforeModel(&callbackCtxAdapter{ctx}, req)
				if resp != nil || err != nil {
					if err != nil {
						yield(nil, err)
						return
					}
					cachedResp = resp
				}
			}
			if cachedResp == nil {
				for _, cb := range a.beforeModelCallbacks {
					resp, err := cb(&callbackCtxAdapter{ctx}, req)
					if resp != nil || err != nil {
						if err != nil {
							yield(nil, err)
							return
						}
						cachedResp = resp
						break
					}
				}
			}

			var finalResp *model.LLMResponse

			if cachedResp != nil {
				finalResp = cachedResp
			} else {
				// Call the model
				stream := ctx.RunConfig() != nil && ctx.RunConfig().StreamingMode == agent.StreamingModeSSE
				for resp, err := range a.model.GenerateContent(ctx, req, stream) {
					if err != nil {
						// Run on-model-error callbacks (plugin hooks first)
						handled := false
						if ph := pluginHooks(ctx); ph != nil {
							r, e := ph.OnModelError(&callbackCtxAdapter{ctx}, req, err)
							if r != nil || e != nil {
								if e != nil {
									yield(nil, e)
									return
								}
								finalResp = r
								handled = true
							}
						}
						if !handled {
							for _, cb := range a.onModelErrorCallbacks {
								r, e := cb(&callbackCtxAdapter{ctx}, req, err)
								if r != nil || e != nil {
									if e != nil {
										yield(nil, e)
										return
									}
									finalResp = r
									handled = true
									break
								}
							}
						}
						if !handled {
							yield(nil, fmt.Errorf("model error: %w", err))
							return
						}
						break
					}

					if resp.Partial && resp.Content != nil {
						// Emit partial chunk to event bus
						text := extractText(resp.Content)
						if text != "" {
							bus.Emit(agent.BusEvent{Type: agent.EventChunk, Agent: a.Name(), Payload: text})

							// Yield partial event
							partialEvent := session.NewEvent(ctx.InvocationID())
							partialEvent.Author = a.Name()
							partialEvent.Branch = ctx.Branch()
							partialEvent.LLMResponse = *resp
							if !yield(partialEvent, nil) {
								return
							}
						}
					}

					if !resp.Partial {
						finalResp = resp
					}
				}
			}

			if finalResp == nil {
				return
			}

			// Run after-model callbacks (plugin hooks first, then agent-level)
			if ph := pluginHooks(ctx); ph != nil {
				r, err := ph.AfterModel(&callbackCtxAdapter{ctx}, finalResp)
				if r != nil || err != nil {
					if err != nil {
						yield(nil, err)
						return
					}
					finalResp = r
				}
			}
			for _, cb := range a.afterModelCallbacks {
				r, err := cb(&callbackCtxAdapter{ctx}, finalResp, nil)
				if r != nil || err != nil {
					if err != nil {
						yield(nil, err)
						return
					}
					finalResp = r
					break
				}
			}

			// Check for function calls
			populateClientFunctionCallID(finalResp.Content)
			functionCalls := extractFunctionCalls(finalResp)

			if len(functionCalls) == 0 {
				// Final text response — yield and exit
				event := session.NewEvent(ctx.InvocationID())
				event.Author = a.Name()
				event.Branch = ctx.Branch()
				event.LLMResponse = *finalResp
				a.maybeSaveOutputToState(event)
				yield(event, nil)
				bus.Emit(agent.BusEvent{Type: agent.EventRunCompleted, Agent: a.Name()})
				return
			}

			// Execute tool calls
			// Yield the function call event first
			callEvent := session.NewEvent(ctx.InvocationID())
			callEvent.Author = a.Name()
			callEvent.Branch = ctx.Branch()
			callEvent.LLMResponse = *finalResp
			if !yield(callEvent, nil) {
				return
			}

			// Execute tool calls in parallel (A1: ADK parity)
			type toolResult struct {
				idx    int
				part   *genai.Part
				action *session.EventActions
			}

			results := make([]toolResult, len(functionCalls))
			var wg sync.WaitGroup

			for i, fc := range functionCalls {
				wg.Add(1)
				go func(idx int, fc *genai.FunctionCall) {
					defer wg.Done()

					bus.Emit(agent.BusEvent{Type: agent.EventToolCall, Agent: a.Name(), Payload: fc.Name})

					result, actions, err := a.executeTool(ctx, fc)
					if err != nil {
						results[idx] = toolResult{
							idx: idx,
							part: &genai.Part{
								FunctionResponse: &genai.FunctionResponse{
									ID:       fc.ID,
									Name:     fc.Name,
									Response: map[string]any{"error": err.Error()},
								},
							},
							action: actions,
						}
						bus.Emit(agent.BusEvent{Type: agent.EventToolResult, Agent: a.Name(), Payload: map[string]string{"tool": fc.Name, "status": "error"}})
					} else {
						results[idx] = toolResult{
							idx: idx,
							part: &genai.Part{
								FunctionResponse: &genai.FunctionResponse{
									ID:       fc.ID,
									Name:     fc.Name,
									Response: result,
								},
							},
							action: actions,
						}
						bus.Emit(agent.BusEvent{Type: agent.EventToolResult, Agent: a.Name(), Payload: map[string]string{"tool": fc.Name, "status": "ok"}})
					}
				}(i, fc)
			}
			wg.Wait()

			// Merge results and actions
			var functionResponses []*genai.Part
			mergedActions := &session.EventActions{StateDelta: make(map[string]any)}
			for _, r := range results {
				functionResponses = append(functionResponses, r.part)
				if r.action != nil {
					if r.action.TransferToAgent != "" {
						mergedActions.TransferToAgent = r.action.TransferToAgent
					}
					if r.action.Escalate {
						mergedActions.Escalate = true
					}
					if r.action.SkipSummarization {
						mergedActions.SkipSummarization = true
					}
					for k, v := range r.action.StateDelta {
						mergedActions.StateDelta[k] = v
					}
				}
			}

			// Yield function response event
			responseEvent := session.NewEvent(ctx.InvocationID())
			responseEvent.Author = a.Name()
			responseEvent.Branch = ctx.Branch()
			responseEvent.Actions = *mergedActions
			responseEvent.LLMResponse = model.LLMResponse{
				Content: &genai.Content{
					Role:  "user",
					Parts: functionResponses,
				},
			}
			if !yield(responseEvent, nil) {
				return
			}

			// Append tool call + response to req.Contents for next iteration.
			// Because buildRequest is called once above the loop, these accumulate
			// across iterations so the model sees the full tool call history.
			req.Contents = append(req.Contents, finalResp.Content)
			req.Contents = append(req.Contents, &genai.Content{
				Role:  "function",
				Parts: functionResponses,
			})
			// Loop continues — model will see the function results
		}
	}
}

func (a *llmAgent) buildRequest(ctx agent.InvocationContext) (*model.LLMRequest, error) {
	req := &model.LLMRequest{
		Model:  a.model.Name(),
		Config: a.generateContentConfig,
	}

	// Build system instruction
	instruction := a.instruction
	if a.instructionProvider != nil {
		var err error
		instruction, err = a.instructionProvider(&callbackCtxAdapter{ctx})
		if err != nil {
			return nil, fmt.Errorf("instruction provider error: %w", err)
		}
	} else if instruction != "" {
		// Template substitution: replace {key} with state values
		instruction = a.resolveTemplate(instruction, ctx)
	}

	// Prepend global instruction if this is a root agent context
	if a.globalInstructionProvider != nil {
		global, err := a.globalInstructionProvider(&callbackCtxAdapter{ctx})
		if err != nil {
			return nil, fmt.Errorf("global instruction provider error: %w", err)
		}
		if global != "" {
			instruction = global + "\n\n" + instruction
		}
	} else if a.globalInstruction != "" {
		instruction = a.globalInstruction + "\n\n" + instruction
	}


	if instruction != "" {
		req.SystemInstruction = &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: instruction}},
		}
	}

	// Build contents from user message
	if ctx.UserContent() != nil {
		req.Contents = []*genai.Content{ctx.UserContent()}
	}

	// Include conversation history from session events
	if a.includeContents != IncludeContentsNone {
		var history []*genai.Content
		for event := range ctx.Session().Events().All() {
			if event.Content != nil {
				history = append(history, event.Content)
			}
		}
		if len(history) > 0 {
			req.Contents = append(history, req.Contents...)
		}
	}

	// Tools are packed per-iteration via packTools(), not here.
	// This allows lazy toolsets to activate tools mid-conversation.

	// Add single transfer_to_agent tool if there are transfer targets
	targets := a.transferTargets(ctx)
	if len(targets) > 0 {
		var targetNames []string
		for _, t := range targets {
			targetNames = append(targetNames, t.Name())
		}
		transferDecl := &genai.FunctionDeclaration{
			Name:        "transfer_to_agent",
			Description: "Transfer the question to another agent when it's more suitable. Available agents: " + strings.Join(targetNames, ", "),
			Parameters: &genai.Schema{
				Type: "OBJECT",
				Properties: map[string]*genai.Schema{
					"agent_name": {
						Type:        "STRING",
						Description: "the agent name to transfer to",
						Enum:        targetNames,
					},
				},
				Required: []string{"agent_name"},
			},
		}
		req.Tools = append(req.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{transferDecl},
		})

		// Inject transfer instructions into system prompt
		var sb strings.Builder
		sb.WriteString("\n\nYou have a list of other agents to transfer to:\n\n")
		for _, t := range targets {
			sb.WriteString(fmt.Sprintf("Agent name: %s\nAgent description: %s\n\n", t.Name(), t.Description()))
		}
		sb.WriteString("If another agent is better for answering the question, call `transfer_to_agent` to transfer. ")
		sb.WriteString("When transferring, do not generate any text other than the function call.\n")
		if req.SystemInstruction != nil && len(req.SystemInstruction.Parts) > 0 {
			req.SystemInstruction.Parts[0].Text += sb.String()
		}
	}

	// Output schema
	if a.outputSchema != nil {
		if req.Config == nil {
			req.Config = &genai.GenerateContentConfig{}
		}
		req.Config.ResponseMIMEType = "application/json"
		req.Config.ResponseSchema = a.outputSchema
	}

	return req, nil
}

func (a *llmAgent) collectTools(ctx agent.InvocationContext) []tool.Tool {
	var all []tool.Tool
	all = append(all, a.tools...)
	for _, ts := range a.toolsets {
		tools := ts.Tools()
		all = append(all, tools...)
	}
	return all
}

// packTools resolves all tools and packs their declarations into the request.
// Called at the start of each loop iteration so that lazy toolsets (e.g., toolbox)
// can dynamically activate/deactivate tools between iterations.
func (a *llmAgent) packTools(ctx agent.InvocationContext, req *model.LLMRequest) error {
	req.Tools = make([]*genai.Tool, 0)
	allTools := a.collectTools(ctx)
	for _, t := range allTools {
		if rt, ok := t.(tool.RunnableTool); ok {
			decl := rt.Declaration()
			if decl != nil {
				req.Tools = append(req.Tools, &genai.Tool{
					FunctionDeclarations: []*genai.FunctionDeclaration{decl},
				})
			}
		} else if nt, ok := t.(tool.NativeTool); ok {
			if err := nt.ProcessRequest(req); err != nil {
				return fmt.Errorf("native tool %q ProcessRequest error: %w", t.Name(), err)
			}
		}
	}
	return nil
}

func (a *llmAgent) executeTool(ctx agent.InvocationContext, fc *genai.FunctionCall) (map[string]any, *session.EventActions, error) {
	actions := &session.EventActions{StateDelta: make(map[string]any)}

	// Check for agent transfer (single tool pattern)
	if fc.Name == "transfer_to_agent" {
		agentName, _ := fc.Args["agent_name"].(string)
		if agentName == "" {
			return nil, actions, fmt.Errorf("transfer_to_agent: missing agent_name")
		}
		actions.TransferToAgent = agentName
		return map[string]any{"status": "transferring to " + agentName}, actions, nil
	}

	allTools := a.collectTools(ctx)
	for _, t := range allTools {
		if t.Name() == fc.Name {
			if rt, ok := t.(tool.RunnableTool); ok {
				args := fc.Args
				if args == nil {
					args = make(map[string]any)
				}

				// Before-tool callbacks (plugin hooks first, then agent-level)
				if ph := pluginHooks(ctx); ph != nil {
					result, err := ph.BeforeTool(&callbackCtxAdapter{ctx}, t, args)
					if result != nil || err != nil {
						return result, actions, err
					}
				}
				for _, cb := range a.beforeToolCallbacks {
					result, err := cb(&callbackCtxAdapter{ctx}, t, args)
					if result != nil || err != nil {
						return result, actions, err
					}
				}

				result, err := rt.Execute(ctx, args)

				// Check for escalation marker from exit_loop tool
				if err == nil && result != nil {
					if _, ok := result["_escalate"]; ok {
						actions.Escalate = true
						actions.SkipSummarization = true
						delete(result, "_escalate")
					}
				}

				// On-tool-error callbacks (plugin hooks first)
				if err != nil {
					if ph := pluginHooks(ctx); ph != nil {
						r, e := ph.OnToolError(&callbackCtxAdapter{ctx}, t, args, err)
						if r != nil || e != nil {
							return r, actions, e
						}
					}
					for _, cb := range a.onToolErrorCallbacks {
						r, e := cb(&callbackCtxAdapter{ctx}, t, args, err)
						if r != nil || e != nil {
							return r, actions, e
						}
					}
					return nil, actions, err
				}

				// After-tool callbacks (plugin hooks first)
				if ph := pluginHooks(ctx); ph != nil {
					r, e := ph.AfterTool(&callbackCtxAdapter{ctx}, t, args, result)
					if r != nil || e != nil {
						return r, actions, e
					}
				}
				for _, cb := range a.afterToolCallbacks {
					r, e := cb(&callbackCtxAdapter{ctx}, t, args, result, nil)
					if r != nil || e != nil {
						return r, actions, e
					}
				}

				return result, actions, nil
			}
		}
	}

	return nil, actions, fmt.Errorf("tool %q not found", fc.Name)
}

func (a *llmAgent) resolveTemplate(tmpl string, ctx agent.InvocationContext) string {
	state := ctx.Session().State()
	// Simple {key} replacement from state
	result := tmpl
	for k, v := range state.All() {
		placeholder := "{" + k + "}"
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, fmt.Sprint(v))
		}
	}
	return result
}

func (a *llmAgent) maybeSaveOutputToState(event *session.Event) {
	if a.outputKey == "" || event == nil || event.Author != a.Name() {
		return
	}
	if event.Partial || event.Content == nil || len(event.Content.Parts) == 0 {
		return
	}

	text := extractTextNoThoughts(event.Content)
	if text == "" {
		return
	}

	if event.Actions.StateDelta == nil {
		event.Actions.StateDelta = make(map[string]any)
	}
	event.Actions.StateDelta[a.outputKey] = text
}

// --- helpers ---

func extractText(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range content.Parts {
		if p.Text != "" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// extractTextNoThoughts extracts text from content, skipping Thought parts.
// Used for OutputKey state saving — internal reasoning shouldn't be persisted.
func extractTextNoThoughts(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range content.Parts {
		if p.Text != "" && !p.Thought {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

func extractFunctionCalls(resp *model.LLMResponse) []*genai.FunctionCall {
	if resp == nil || resp.Content == nil {
		return nil
	}
	var calls []*genai.FunctionCall
	for _, part := range resp.Content.Parts {
		if part.FunctionCall != nil {
			calls = append(calls, part.FunctionCall)
		}
	}
	return calls
}

// callbackCtxAdapter wraps InvocationContext to satisfy CallbackContext.
type callbackCtxAdapter struct {
	agent.InvocationContext
}

func (c *callbackCtxAdapter) AgentName() string                   { return c.Agent().Name() }
func (c *callbackCtxAdapter) ReadonlyState() session.ReadonlyState { return c.Session().State() }
func (c *callbackCtxAdapter) State() session.State                 { return c.Session().State() }
func (c *callbackCtxAdapter) UserID() string                       { return c.Session().UserID() }
func (c *callbackCtxAdapter) AppName() string                      { return c.Session().AppName() }
func (c *callbackCtxAdapter) SessionID() string                    { return c.Session().ID() }

var _ agent.CallbackContext = (*callbackCtxAdapter)(nil)

// transferTargets computes the set of agents this agent can transfer to:
// - Sub-agents (always)
// - Parent agent (if !DisallowTransferToParent)
// - Peer agents (if !DisallowTransferToPeers and parent has sub-agents)
func (a *llmAgent) transferTargets(ctx agent.InvocationContext) []agent.Agent {
	seen := make(map[string]bool)
	var targets []agent.Agent

	addUnique := func(ag agent.Agent) {
		if !seen[ag.Name()] {
			seen[ag.Name()] = true
			targets = append(targets, ag)
		}
	}

	for _, sub := range a.SubAgents() {
		addUnique(sub)
	}

	parentMap := ctx.ParentMap()
	if parentMap == nil {
		return targets
	}

	parent := parentMap[a.Name()]
	if parent == nil {
		return targets
	}

	// Add parent if allowed
	if !a.disallowTransferToParent {
		addUnique(parent)
	}

	// Add peer agents if allowed
	if !a.disallowTransferToPeers {
		for _, peer := range parent.SubAgents() {
			if peer.Name() != a.Name() {
				addUnique(peer)
			}
		}
	}

	return targets
}

// populateClientFunctionCallID generates UUIDs for function calls that
// are missing an ID. Some models don't populate FunctionCall.ID.
func populateClientFunctionCallID(content *genai.Content) {
	if content == nil {
		return
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil && part.FunctionCall.ID == "" {
			part.FunctionCall.ID = uuid.NewString()
		}
	}
}
