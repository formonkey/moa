package otelplugin

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
)

// testCtx is a minimal CallbackContext for testing.
type testCtx struct {
	context.Context
	name      string
	sessionID string
}

func newTestCtx(name string) *testCtx {
	return &testCtx{Context: context.Background(), name: name, sessionID: "test-session"}
}

func (t *testCtx) AgentName() string                  { return t.name }
func (t *testCtx) AppName() string                    { return "test-app" }
func (t *testCtx) SessionID() string                  { return t.sessionID }
func (t *testCtx) UserID() string                     { return "test-user" }
func (t *testCtx) InvocationID() string               { return "inv-1" }
func (t *testCtx) Branch() string                     { return "" }
func (t *testCtx) UserContent() *genai.Content        { return nil }
func (t *testCtx) ReadonlyState() session.ReadonlyState { return nil }
func (t *testCtx) Artifacts() agent.Artifacts         { return nil }
func (t *testCtx) State() session.State               { return nil }
func (t *testCtx) Deadline() (time.Time, bool)        { return time.Time{}, false }
func (t *testCtx) Err() error                         { return nil }
func (t *testCtx) Value(any) any                      { return nil }

var _ agent.CallbackContext = (*testCtx)(nil)

type testTool struct {
	name string
}

func (t *testTool) Name() string        { return t.name }
func (t *testTool) Description() string { return "" }
func (t *testTool) IsNative() bool      { return false }
func (t *testTool) IsLongRunning() bool { return false }

func newTestTracer() (trace.Tracer, *tracetest.InMemoryExporter) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	return tp.Tracer("test"), exporter
}

func TestNewWithNilTracer(t *testing.T) {
	plug := New(Config{})
	if plug.Name != "otel" {
		t.Errorf("expected name 'otel', got %q", plug.Name)
	}
	// Should be no-op — no callbacks set
	if plug.BeforeRunCallback != nil {
		t.Error("expected nil callbacks with nil tracer")
	}
}

func TestRunSpans(t *testing.T) {
	tracer, exporter := newTestTracer()
	plug := New(Config{Tracer: tracer, RecordTokens: true})

	ctx := newTestCtx("test-agent")

	plug.BeforeRunCallback(ctx)
	plug.AfterRunCallback(ctx)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != "agent.run" {
		t.Errorf("expected span name 'agent.run', got %q", spans[0].Name)
	}

	// Check attributes
	found := false
	for _, attr := range spans[0].Attributes {
		if attr.Key == "agent.name" && attr.Value.AsString() == "test-agent" {
			found = true
		}
	}
	if !found {
		t.Error("expected agent.name attribute")
	}
}

func TestModelCallSpans(t *testing.T) {
	tracer, exporter := newTestTracer()
	plug := New(Config{Tracer: tracer, RecordTokens: true})

	ctx := newTestCtx("test-agent")

	// Start agent span first (parent)
	plug.BeforeRunCallback(ctx)
	plug.BeforeAgentCallback(ctx)

	req := &model.LLMRequest{
		Model: "gemini-2.5-flash",
		Tools: []*genai.Tool{
			{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "read_file"}}},
		},
	}

	plug.BeforeModelCallback(ctx, req)

	resp := &model.LLMResponse{
		FinishReason: "STOP",
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     100,
			CandidatesTokenCount: 50,
			TotalTokenCount:      150,
		},
	}

	plug.AfterModelCallback(ctx, resp)

	// End agent spans
	plug.AfterAgentCallback(ctx)
	plug.AfterRunCallback(ctx)

	spans := exporter.GetSpans()
	// Should have: agent.run, agent.invoke, agent.model_call
	if len(spans) < 3 {
		t.Fatalf("expected at least 3 spans, got %d", len(spans))
	}

	// Find model span
	var modelSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "agent.model_call" {
			modelSpan = &spans[i]
			break
		}
	}
	if modelSpan == nil {
		t.Fatal("expected agent.model_call span")
	}

	// Check token attributes
	hasTokens := false
	for _, attr := range modelSpan.Attributes {
		if attr.Key == "model.input_tokens" {
			hasTokens = true
			if attr.Value.AsInt64() != 100 {
				t.Errorf("expected 100 input tokens, got %d", attr.Value.AsInt64())
			}
		}
	}
	if !hasTokens {
		t.Error("expected token attributes on model span")
	}
}

func TestToolCallSpans(t *testing.T) {
	tracer, exporter := newTestTracer()
	plug := New(Config{Tracer: tracer, IncludeToolArgs: true})

	ctx := newTestCtx("test-agent")
	plug.BeforeRunCallback(ctx)
	plug.BeforeAgentCallback(ctx)

	tt := &testTool{name: "read_file"}
	args := map[string]any{"path": "main.go"}

	plug.BeforeToolCallback(ctx, tt, args)
	result := map[string]any{"content": "package main"}
	plug.AfterToolCallback(ctx, tt, args, result)

	plug.AfterAgentCallback(ctx)
	plug.AfterRunCallback(ctx)

	spans := exporter.GetSpans()

	var toolSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "agent.tool_call" {
			toolSpan = &spans[i]
			break
		}
	}
	if toolSpan == nil {
		t.Fatal("expected agent.tool_call span")
	}

	// Check tool name
	hasName := false
	hasArg := false
	for _, attr := range toolSpan.Attributes {
		if attr.Key == "tool.name" && attr.Value.AsString() == "read_file" {
			hasName = true
		}
		if attr.Key == "tool.args.path" && attr.Value.AsString() == "main.go" {
			hasArg = true
		}
	}
	if !hasName {
		t.Error("expected tool.name attribute")
	}
	if !hasArg {
		t.Error("expected tool.args.path attribute when IncludeToolArgs is true")
	}
}

func TestToolCallErrorSpan(t *testing.T) {
	tracer, exporter := newTestTracer()
	plug := New(Config{Tracer: tracer})

	ctx := newTestCtx("test-agent")
	plug.BeforeRunCallback(ctx)
	plug.BeforeAgentCallback(ctx)

	tt := &testTool{name: "run_command"}
	args := map[string]any{"command": "false"}

	plug.BeforeToolCallback(ctx, tt, args)

	// Simulate error
	plug.OnToolErrorCallback(ctx, tt, args, fmt.Errorf("exit code 1"))

	plug.AfterAgentCallback(ctx)
	plug.AfterRunCallback(ctx)

	spans := exporter.GetSpans()
	var toolSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "agent.tool_call" {
			toolSpan = &spans[i]
			break
		}
	}
	if toolSpan == nil {
		t.Fatal("expected agent.tool_call span")
	}

	if toolSpan.Status.Code != 1 { // codes.Error = 1
		t.Errorf("expected error status, got %d", toolSpan.Status.Code)
	}
}

func TestModelErrorSpan(t *testing.T) {
	tracer, exporter := newTestTracer()
	plug := New(Config{Tracer: tracer})

	ctx := newTestCtx("test-agent")
	plug.BeforeRunCallback(ctx)
	plug.BeforeAgentCallback(ctx)

	req := &model.LLMRequest{Model: "gemini-2.5-flash"}
	plug.BeforeModelCallback(ctx, req)
	plug.OnModelErrorCallback(ctx, req, fmt.Errorf("rate limit"))

	plug.AfterAgentCallback(ctx)
	plug.AfterRunCallback(ctx)

	spans := exporter.GetSpans()
	var modelSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "agent.model_call" {
			modelSpan = &spans[i]
			break
		}
	}
	if modelSpan == nil {
		t.Fatal("expected agent.model_call span")
	}
	if modelSpan.Status.Code != 1 {
		t.Errorf("expected error status, got %d", modelSpan.Status.Code)
	}
}

func hasAttr(attrs []attribute.KeyValue, key string) bool {
	for _, a := range attrs {
		if string(a.Key) == key {
			return true
		}
	}
	return false
}
