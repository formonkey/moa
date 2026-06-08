package telemetry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/formonkey/moa/telemetry"
)

func TestNew(t *testing.T) {
	p, err := telemetry.New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("nil providers")
	}
	if p.TracerProvider == nil {
		t.Fatal("nil tracer provider")
	}
}

func TestNewWithCaptureContent(t *testing.T) {
	p, err := telemetry.New(context.Background(), telemetry.WithCaptureContent(true))
	if err != nil {
		t.Fatal(err)
	}
	if !p.CaptureContent {
		t.Fatal("expected CaptureContent=true")
	}
}

func TestNewWithTracerProvider(t *testing.T) {
	p, err := telemetry.New(context.Background(), telemetry.WithTracerProvider(nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = p
}

func TestTracer(t *testing.T) {
	tr := telemetry.Tracer()
	if tr == nil {
		t.Fatal("nil tracer")
	}
}

func TestStartSpan(t *testing.T) {
	ctx, span := telemetry.StartSpan(context.Background(), "test-span")
	if ctx == nil {
		t.Fatal("nil context")
	}
	if span == nil {
		t.Fatal("nil span")
	}
	span.End()
}

func TestStartAgentSpan(t *testing.T) {
	ctx, span := telemetry.StartAgentSpan(context.Background(), "my-agent", "inv-1")
	if ctx == nil || span == nil {
		t.Fatal("nil")
	}
	span.End()
}

func TestStartModelSpan(t *testing.T) {
	ctx, span := telemetry.StartModelSpan(context.Background(), "gpt-4", "my-agent")
	if ctx == nil || span == nil {
		t.Fatal("nil")
	}
	span.End()
}

func TestStartToolSpan(t *testing.T) {
	ctx, span := telemetry.StartToolSpan(context.Background(), "search", "my-agent")
	if ctx == nil || span == nil {
		t.Fatal("nil")
	}
	span.End()
}

func TestStartFSMSpan(t *testing.T) {
	ctx, span := telemetry.StartFSMSpan(context.Background(), "fsm-agent", "initial")
	if ctx == nil || span == nil {
		t.Fatal("nil")
	}
	span.End()
}

func TestStartSwarmSpan(t *testing.T) {
	ctx, span := telemetry.StartSwarmSpan(context.Background(), "my-swarm")
	if ctx == nil || span == nil {
		t.Fatal("nil")
	}
	span.End()
}

func TestRecordError(t *testing.T) {
	_, span := telemetry.StartSpan(context.Background(), "err-span")
	telemetry.RecordError(span, errors.New("test error"))
	span.End()
}

func TestRecordErrorNil(t *testing.T) {
	_, span := telemetry.StartSpan(context.Background(), "nil-err-span")
	telemetry.RecordError(span, nil)
	telemetry.RecordError(nil, errors.New("no span"))
	span.End()
}
