package telemetry

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// SetupConfig configures the auto-setup.
type SetupConfig struct {
	// ServiceName identifies this application in traces. Default: "moa".
	ServiceName string
	// ServiceVersion is the version of the application.
	ServiceVersion string
	// StdoutTrace sends traces to stdout (useful for debugging).
	StdoutTrace bool
	// StdoutWriter overrides where stdout traces go. Default: os.Stdout.
	StdoutWriter io.Writer
}

// SetupOption configures auto-setup.
type SetupOption func(*SetupConfig)

// WithServiceName sets the service name for traces.
func WithServiceName(name string) SetupOption {
	return func(c *SetupConfig) { c.ServiceName = name }
}

// WithServiceVersion sets the service version for traces.
func WithServiceVersion(version string) SetupOption {
	return func(c *SetupConfig) { c.ServiceVersion = version }
}

// WithStdoutTrace enables stdout trace export.
func WithStdoutTrace(w io.Writer) SetupOption {
	return func(c *SetupConfig) {
		c.StdoutTrace = true
		c.StdoutWriter = w
	}
}

// Setup initializes OpenTelemetry with sensible defaults.
//
// It auto-detects exporters from environment variables:
//   - OTEL_EXPORTER_OTLP_ENDPOINT → OTLP HTTP exporter (Jaeger, Grafana Tempo, etc.)
//   - MOA_TELEMETRY_STDOUT=true   → Stdout exporter (debugging)
//
// Returns a shutdown function that must be called on application exit.
func Setup(ctx context.Context, opts ...SetupOption) (shutdown func(context.Context) error, err error) {
	cfg := &SetupConfig{
		ServiceName: "moa",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Check env overrides
	if strings.TrimSpace(os.Getenv("MOA_TELEMETRY_STDOUT")) == "true" {
		cfg.StdoutTrace = true
	}

	// Build resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: failed to create resource: %w", err)
	}
	if cfg.ServiceVersion != "" {
		versionRes, _ := resource.New(ctx,
			resource.WithAttributes(
				semconv.ServiceVersionKey.String(cfg.ServiceVersion),
			),
		)
		res, _ = resource.Merge(res, versionRes)
	}

	var spanProcessors []sdktrace.SpanProcessor

	// OTLP HTTP exporter (auto-detected from env)
	otlpEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	tracesEndpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
	if otlpEndpoint != "" || tracesEndpoint != "" {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("telemetry: failed to create OTLP HTTP exporter: %w", err)
		}
		spanProcessors = append(spanProcessors, sdktrace.NewBatchSpanProcessor(exporter))
	}

	// Stdout exporter (debugging)
	if cfg.StdoutTrace {
		w := cfg.StdoutWriter
		if w == nil {
			w = os.Stdout
		}
		exporter, err := stdouttrace.New(stdouttrace.WithWriter(w))
		if err != nil {
			return nil, fmt.Errorf("telemetry: failed to create stdout exporter: %w", err)
		}
		spanProcessors = append(spanProcessors, sdktrace.NewSimpleSpanProcessor(exporter))
	}

	if len(spanProcessors) == 0 {
		// No exporters configured — noop
		return func(context.Context) error { return nil }, nil
	}

	// Build TracerProvider
	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
	}
	for _, sp := range spanProcessors {
		tpOpts = append(tpOpts, sdktrace.WithSpanProcessor(sp))
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)

	// Set as global
	otel.SetTracerProvider(tp)

	shutdown = func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}
	return shutdown, nil
}
