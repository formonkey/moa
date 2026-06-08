// Package otelplugin provides an OpenTelemetry tracing plugin for moa agents.
//
// It creates spans for every agent run, model call, and tool execution,
// giving full observability into the agent's decision-making pipeline.
//
// Usage:
//
//	plug := otelplugin.New(otelplugin.Config{
//	    Tracer: otel.Tracer("moa"),
//	})
//	runner, _ := runner.New(runner.Config{Plugins: []*plugin.Plugin{plug}})
package otelplugin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin"
	"github.com/formonkey/moa/session"
	"github.com/formonkey/moa/tool"
)

// Config for the OTel tracing plugin.
type Config struct {
	// Tracer is the OpenTelemetry tracer to use. Required.
	Tracer trace.Tracer

	// RecordTokens records input/output token counts as span attributes.
	// Default: true.
	RecordTokens bool

	// IncludeToolArgs includes tool arguments in span attributes.
	// WARNING: may expose PII. Default: false.
	IncludeToolArgs bool

	// IncludeModelContent includes model request/response text in spans.
	// WARNING: may expose PII and consume storage. Default: false.
	IncludeModelContent bool
}

// spanStore holds active spans keyed by agent name.
// This is needed because plugin callbacks don't carry span context natively.
type spanStore struct {
	mu    sync.RWMutex
	spans map[string]spanEntry
}

type spanEntry struct {
	span  trace.Span
	ctx   context.Context
	start time.Time
}

func newSpanStore() *spanStore {
	return &spanStore{spans: make(map[string]spanEntry)}
}

func (s *spanStore) set(key string, ctx context.Context, span trace.Span) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spans[key] = spanEntry{span: span, ctx: ctx, start: time.Now()}
}

func (s *spanStore) get(key string) (context.Context, trace.Span, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.spans[key]
	if !ok {
		return context.Background(), nil, false
	}
	return e.ctx, e.span, true
}

func (s *spanStore) remove(key string) (trace.Span, time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.spans[key]
	if !ok {
		return nil, 0
	}
	delete(s.spans, key)
	return e.span, time.Since(e.start)
}

// New creates an OpenTelemetry tracing plugin.
func New(cfg Config) *plugin.Plugin {
	if cfg.Tracer == nil {
		// Return a no-op plugin if no tracer is provided
		return &plugin.Plugin{Name: "otel"}
	}

	agentSpans := newSpanStore()
	modelSpans := newSpanStore()
	toolSpans := newSpanStore()

	return &plugin.Plugin{
		Name: "otel",

		BeforeRunCallback: func(ctx agent.CallbackContext) error {
			_, span := cfg.Tracer.Start(context.Background(), "agent.run",
				trace.WithAttributes(
					attribute.String("agent.name", ctx.AgentName()),
					attribute.String("session.id", ctx.SessionID()),
				),
			)
			agentSpans.set("run:"+ctx.AgentName(), context.Background(), span)
			return nil
		},

		AfterRunCallback: func(ctx agent.CallbackContext) error {
			span, dur := agentSpans.remove("run:" + ctx.AgentName())
			if span != nil {
				span.SetAttributes(attribute.Int64("duration_ms", dur.Milliseconds()))
				span.SetStatus(codes.Ok, "completed")
				span.End()
			}
			return nil
		},

		BeforeAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			parentCtx, _, ok := agentSpans.get("run:" + ctx.AgentName())
			if !ok {
				parentCtx = context.Background()
			}

			_, span := cfg.Tracer.Start(parentCtx, "agent.invoke",
				trace.WithAttributes(
					attribute.String("agent.name", ctx.AgentName()),
				),
			)
			agentSpans.set("invoke:"+ctx.AgentName(), parentCtx, span)
			return nil, nil
		},

		AfterAgentCallback: func(ctx agent.CallbackContext) (*session.Event, error) {
			span, _ := agentSpans.remove("invoke:" + ctx.AgentName())
			if span != nil {
				span.End()
			}
			return nil, nil
		},

		BeforeModelCallback: func(ctx agent.CallbackContext, req *model.LLMRequest) (*model.LLMResponse, error) {
			parentCtx, _, ok := agentSpans.get("invoke:" + ctx.AgentName())
			if !ok {
				parentCtx = context.Background()
			}

			attrs := []attribute.KeyValue{
				attribute.String("model.name", req.Model),
				attribute.Int("model.contents_count", len(req.Contents)),
			}

			// Count tools
			toolCount := 0
			for _, t := range req.Tools {
				if t.FunctionDeclarations != nil {
					toolCount += len(t.FunctionDeclarations)
				}
			}
			attrs = append(attrs, attribute.Int("model.tools_count", toolCount))

			if cfg.IncludeModelContent && len(req.Contents) > 0 {
				last := req.Contents[len(req.Contents)-1]
				if last != nil && len(last.Parts) > 0 && last.Parts[0].Text != "" {
					text := last.Parts[0].Text
					if len(text) > 500 {
						text = text[:500] + "..."
					}
					attrs = append(attrs, attribute.String("model.input_text", text))
				}
			}

			_, span := cfg.Tracer.Start(parentCtx, "agent.model_call",
				trace.WithAttributes(attrs...),
			)
			modelSpans.set(ctx.AgentName(), parentCtx, span)
			return nil, nil
		},

		AfterModelCallback: func(ctx agent.CallbackContext, resp *model.LLMResponse) (*model.LLMResponse, error) {
			span, dur := modelSpans.remove(ctx.AgentName())
			if span != nil {
				span.SetAttributes(attribute.Int64("duration_ms", dur.Milliseconds()))

				if resp != nil {
					span.SetAttributes(attribute.String("model.finish_reason", string(resp.FinishReason)))

					if cfg.RecordTokens && resp.UsageMetadata != nil {
						span.SetAttributes(
							attribute.Int("model.input_tokens", int(resp.UsageMetadata.PromptTokenCount)),
							attribute.Int("model.output_tokens", int(resp.UsageMetadata.CandidatesTokenCount)),
							attribute.Int("model.total_tokens", int(resp.UsageMetadata.TotalTokenCount)),
						)
					}

					if cfg.IncludeModelContent && resp.Content != nil {
						var text string
						for _, p := range resp.Content.Parts {
							if p.Text != "" {
								text += p.Text
							}
						}
						if len(text) > 500 {
							text = text[:500] + "..."
						}
						if text != "" {
							span.SetAttributes(attribute.String("model.output_text", text))
						}
					}
				}

				span.SetStatus(codes.Ok, "")
				span.End()
			}
			return nil, nil
		},

		OnModelErrorCallback: func(ctx agent.CallbackContext, req *model.LLMRequest, err error) (*model.LLMResponse, error) {
			span, _ := modelSpans.remove(ctx.AgentName())
			if span != nil {
				span.SetStatus(codes.Error, err.Error())
				span.RecordError(err)
				span.End()
			}
			return nil, nil
		},

		BeforeToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any) (map[string]any, error) {
			parentCtx, _, ok := agentSpans.get("invoke:" + ctx.AgentName())
			if !ok {
				parentCtx = context.Background()
			}

			attrs := []attribute.KeyValue{
				attribute.String("tool.name", t.Name()),
				attribute.String("agent.name", ctx.AgentName()),
			}

			if cfg.IncludeToolArgs && args != nil {
				for k, v := range args {
					attrs = append(attrs, attribute.String(
						fmt.Sprintf("tool.args.%s", k),
						fmt.Sprintf("%v", v),
					))
				}
			}

			_, span := cfg.Tracer.Start(parentCtx, "agent.tool_call",
				trace.WithAttributes(attrs...),
			)
			toolKey := ctx.AgentName() + ":" + t.Name()
			toolSpans.set(toolKey, parentCtx, span)
			return nil, nil
		},

		AfterToolCallback: func(ctx agent.CallbackContext, t tool.Tool, args, result map[string]any) (map[string]any, error) {
			toolKey := ctx.AgentName() + ":" + t.Name()
			span, dur := toolSpans.remove(toolKey)
			if span != nil {
				span.SetAttributes(attribute.Int64("duration_ms", dur.Milliseconds()))

				if result != nil {
					// Record result keys (not values to avoid PII)
					var keys []string
					for k := range result {
						keys = append(keys, k)
					}
					span.SetAttributes(attribute.String("tool.result_keys", strings.Join(keys, ",")))
				}

				span.SetStatus(codes.Ok, "")
				span.End()
			}
			return nil, nil
		},

		OnToolErrorCallback: func(ctx agent.CallbackContext, t tool.Tool, args map[string]any, err error) (map[string]any, error) {
			toolKey := ctx.AgentName() + ":" + t.Name()
			span, _ := toolSpans.remove(toolKey)
			if span != nil {
				span.SetStatus(codes.Error, err.Error())
				span.RecordError(err)
				span.End()
			}
			return nil, nil
		},
	}
}
