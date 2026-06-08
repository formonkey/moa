// Package telemetry provides OpenTelemetry integration for moa.
//
// It offers a Providers struct for managing TracerProvider and LoggerProvider
// lifecycle, plus specialized span helpers for agent, model, and tool invocations.
//
// Setup is automatic: if OTEL_EXPORTER_OTLP_ENDPOINT is set, traces and logs
// are exported via OTLP HTTP. For stdout debugging, set MOA_TELEMETRY_STDOUT=true.
package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/trace"
)

const TracerName = "github.com/formonkey/moa"

// Providers holds the OpenTelemetry providers used by moa.
type Providers struct {
	TracerProvider sdktrace.TracerProvider
	// CaptureContent controls whether LLM message content is logged in spans.
	CaptureContent bool
}

// Option configures Providers during initialization.
type Option func(*options)

type options struct {
	tracerProvider sdktrace.TracerProvider
	captureContent bool
}

// WithTracerProvider sets a custom TracerProvider.
func WithTracerProvider(tp sdktrace.TracerProvider) Option {
	return func(o *options) { o.tracerProvider = tp }
}

// WithCaptureContent enables logging of LLM message content in spans.
func WithCaptureContent(capture bool) Option {
	return func(o *options) { o.captureContent = capture }
}

// New creates a new Providers with the given options.
// If no TracerProvider is set, it uses the global OTel TracerProvider.
func New(_ context.Context, opts ...Option) (*Providers, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	tp := o.tracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}

	return &Providers{
		TracerProvider: tp,
		CaptureContent: o.captureContent,
	}, nil
}

// Tracer returns the standard OpenTelemetry tracer for moa.
func Tracer() sdktrace.Tracer {
	return otel.Tracer(TracerName)
}

// StartSpan starts a new OTEL span.
func StartSpan(ctx context.Context, name string) (context.Context, sdktrace.Span) {
	return Tracer().Start(ctx, name)
}

// --- Specialized spans for agent lifecycle ---

// StartAgentSpan starts a span for an agent invocation.
func StartAgentSpan(ctx context.Context, agentName, invocationID string) (context.Context, sdktrace.Span) {
	ctx, span := Tracer().Start(ctx, fmt.Sprintf("agent.%s", agentName))
	span.SetAttributes(
		attribute.String("moa.agent.name", agentName),
		attribute.String("moa.invocation.id", invocationID),
	)
	return ctx, span
}

// StartModelSpan starts a span for a model call.
func StartModelSpan(ctx context.Context, modelName, agentName string) (context.Context, sdktrace.Span) {
	ctx, span := Tracer().Start(ctx, fmt.Sprintf("model.%s", modelName))
	span.SetAttributes(
		attribute.String("moa.model.name", modelName),
		attribute.String("moa.agent.name", agentName),
	)
	return ctx, span
}

// StartToolSpan starts a span for a tool execution.
func StartToolSpan(ctx context.Context, toolName, agentName string) (context.Context, sdktrace.Span) {
	ctx, span := Tracer().Start(ctx, fmt.Sprintf("tool.%s", toolName))
	span.SetAttributes(
		attribute.String("moa.tool.name", toolName),
		attribute.String("moa.agent.name", agentName),
	)
	return ctx, span
}

// StartFSMSpan starts a span for an FSM state transition.
func StartFSMSpan(ctx context.Context, agentName, stateName string) (context.Context, sdktrace.Span) {
	ctx, span := Tracer().Start(ctx, fmt.Sprintf("fsm.%s.%s", agentName, stateName))
	span.SetAttributes(
		attribute.String("moa.agent.name", agentName),
		attribute.String("moa.fsm.state", stateName),
	)
	return ctx, span
}

// StartSwarmSpan starts a span for a swarm execution.
func StartSwarmSpan(ctx context.Context, swarmName string) (context.Context, sdktrace.Span) {
	ctx, span := Tracer().Start(ctx, fmt.Sprintf("swarm.%s", swarmName))
	span.SetAttributes(
		attribute.String("moa.swarm.name", swarmName),
	)
	return ctx, span
}

// RecordError records an error on a span.
func RecordError(span sdktrace.Span, err error) {
	if err != nil && span != nil {
		span.RecordError(err)
	}
}
