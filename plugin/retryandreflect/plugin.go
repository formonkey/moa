// Package retryandreflect provides a self-healing, concurrent-safe error
// recovery plugin for tool failures.
//
// When a tool fails, the plugin constructs a structured reflection prompt
// containing the error details and original arguments, then returns it as the
// tool response so the LLM can self-correct and retry.
//
// Features:
//   - Template-based reflection and exceeded messages (embedded Go templates)
//   - Scoped failure tracking: per-invocation or global
//   - Thread-safe failure counters
//   - Configurable max retries and error-on-exceed behavior
//
// Inspired by ADK Go's retryandreflect plugin.
package retryandreflect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"text/template"

	_ "embed"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/tool"
)

//go:embed reflection.md
var reflection string
var reflectionTemplate = template.Must(template.New("ReflectionTemplate").Parse(reflection))

//go:embed exceeded.md
var exceeded string
var exceededTemplate = template.Must(template.New("ExceededTemplate").Parse(exceeded))

const (
	reflectAndRetryResponseType = "ERROR_HANDLED_BY_REFLECT_AND_RETRY_PLUGIN"
	globalScopeKey              = "__global_reflect_and_retry_scope__"
)

// TrackingScope defines the lifecycle scope for tracking tool failure counts.
type TrackingScope string

const (
	// Invocation tracks failures per-invocation (reset each new Run()).
	Invocation TrackingScope = "invocation"
	// Global tracks failures globally across all turns and users.
	Global TrackingScope = "global"
)

type retryAndReflect struct {
	mu                    sync.Mutex
	maxRetries            int
	errorIfRetryExceeded  bool
	scope                 TrackingScope
	scopedFailureCounters map[string]map[string]int
}

// Option configures the retryandreflect plugin.
type Option func(*retryAndReflect)

// WithMaxRetries sets the maximum number of retries per tool. Default: 3.
func WithMaxRetries(n int) Option {
	return func(r *retryAndReflect) { r.maxRetries = n }
}

// WithErrorIfRetryExceeded controls whether to propagate the original error
// when retries are exhausted. If false (default), a "stop using this tool"
// instruction is sent to the LLM instead.
func WithErrorIfRetryExceeded(v bool) Option {
	return func(r *retryAndReflect) { r.errorIfRetryExceeded = v }
}

// WithTrackingScope sets whether failure counters are per-invocation or global.
func WithTrackingScope(scope TrackingScope) Option {
	return func(r *retryAndReflect) { r.scope = scope }
}

// New creates a new retry-and-reflect plugin.
func New(opts ...Option) *plugin.Plugin {
	r := &retryAndReflect{
		maxRetries:            3,
		errorIfRetryExceeded:  false,
		scope:                 Invocation,
		scopedFailureCounters: make(map[string]map[string]int),
	}
	for _, opt := range opts {
		opt(r)
	}

	return &plugin.Plugin{
		Name: "retry-and-reflect",
		AfterToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error) {
			// On success, reset the failure count unless this IS a reflection response.
			isReflect := false
			if rt, ok := result["response_type"].(string); ok && rt == reflectAndRetryResponseType {
				isReflect = true
			}
			if !isReflect {
				r.resetFailuresForTool(ctx, t.Name())
			}
			return nil, nil
		},
		OnToolErrorCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
			return r.handleToolError(ctx, t, args, err)
		},
	}
}

func (r *retryAndReflect) handleToolError(ctx agent.CallbackContext, failedTool tool.Tool, args map[string]any, err error) (map[string]any, error) {
	// Skip confirmation-related errors — they are not real failures.
	if err != nil && (err.Error() == tool.ErrConfirmationRequired.Error() || err.Error() == tool.ErrConfirmationRejected.Error()) {
		return nil, nil
	}

	if r.maxRetries == 0 {
		if r.errorIfRetryExceeded {
			return nil, err
		}
		return r.createToolRetryExceedMsg(failedTool, args, err), nil
	}

	scopeKey := r.scopeKey(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()

	toolFailureCounter, ok := r.scopedFailureCounters[scopeKey]
	if !ok {
		toolFailureCounter = make(map[string]int)
		r.scopedFailureCounters[scopeKey] = toolFailureCounter
	}

	currentRetries := toolFailureCounter[failedTool.Name()] + 1
	toolFailureCounter[failedTool.Name()] = currentRetries

	if currentRetries <= r.maxRetries {
		return r.createToolReflectionResponse(failedTool, args, err, currentRetries), nil
	}

	// Max retry exceeded
	if r.errorIfRetryExceeded {
		return nil, err
	}
	return r.createToolRetryExceedMsg(failedTool, args, err), nil
}

func (r *retryAndReflect) scopeKey(ctx agent.CallbackContext) string {
	if r.scope == Global {
		return globalScopeKey
	}
	return ctx.InvocationID()
}

func (r *retryAndReflect) resetFailuresForTool(ctx agent.CallbackContext, toolName string) {
	scopeKey := r.scopeKey(ctx)

	r.mu.Lock()
	defer r.mu.Unlock()
	if scope, ok := r.scopedFailureCounters[scopeKey]; ok {
		delete(scope, toolName)
		if len(scope) == 0 {
			delete(r.scopedFailureCounters, scopeKey)
		}
	}
}

// templateData represents the variables in the templates.
type templateData struct {
	ToolName     string
	ErrorDetails string
	ArgsSummary  string
	RetryCount   int
	MaxRetries   int
}

func (r *retryAndReflect) formatErrorDetails(err error) string {
	return fmt.Sprintf("%T: %v", err, err)
}

func (r *retryAndReflect) formatToolArgs(toolArgs map[string]any) string {
	argsBytes, err := json.MarshalIndent(toolArgs, "", "  ")
	if err != nil {
		return fmt.Sprintf("%+v", toolArgs)
	}
	return string(argsBytes)
}

func (r *retryAndReflect) createToolReflectionResponse(t tool.Tool, toolArgs map[string]any, toolErr error, retryCount int) map[string]any {
	d := templateData{
		ToolName:     t.Name(),
		ErrorDetails: r.formatErrorDetails(toolErr),
		ArgsSummary:  r.formatToolArgs(toolArgs),
		RetryCount:   retryCount,
		MaxRetries:   r.maxRetries,
	}

	var buf bytes.Buffer
	if err := reflectionTemplate.Execute(&buf, d); err != nil {
		return map[string]any{
			"response_type": reflectAndRetryResponseType,
			"error_details": toolErr.Error(),
		}
	}

	return map[string]any{
		"response_type":       reflectAndRetryResponseType,
		"error_type":          fmt.Sprintf("%T", toolErr),
		"error_details":       toolErr.Error(),
		"retry_count":         retryCount,
		"reflection_guidance": strings.TrimSpace(buf.String()),
	}
}

func (r *retryAndReflect) createToolRetryExceedMsg(t tool.Tool, toolArgs map[string]any, toolErr error) map[string]any {
	d := templateData{
		ToolName:     t.Name(),
		ErrorDetails: r.formatErrorDetails(toolErr),
		ArgsSummary:  r.formatToolArgs(toolArgs),
		MaxRetries:   r.maxRetries,
	}

	var buf bytes.Buffer
	if err := exceededTemplate.Execute(&buf, d); err != nil {
		return map[string]any{
			"response_type": reflectAndRetryResponseType,
			"error_details": toolErr.Error(),
		}
	}

	return map[string]any{
		"response_type":       reflectAndRetryResponseType,
		"error_type":          fmt.Sprintf("%T", toolErr),
		"error_details":       toolErr.Error(),
		"retry_count":         r.maxRetries,
		"reflection_guidance": strings.TrimSpace(buf.String()),
	}
}
