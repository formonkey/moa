package telemetry_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/formonkey/moa/telemetry"
)

func TestSetup(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if shutdown == nil {
		t.Fatal("nil shutdown")
	}
	shutdown(context.Background())
}

func TestSetupWithServiceName(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background(), telemetry.WithServiceName("test-service"))
	if err != nil {
		t.Fatal(err)
	}
	shutdown(context.Background())
}

func TestSetupWithServiceVersion(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background(),
		telemetry.WithServiceName("svc"),
		telemetry.WithServiceVersion("1.0.0"),
	)
	if err != nil {
		t.Fatal(err)
	}
	shutdown(context.Background())
}

func TestSetupWithStdoutTrace(t *testing.T) {
	var buf bytes.Buffer
	shutdown, err := telemetry.Setup(context.Background(), telemetry.WithStdoutTrace(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if shutdown == nil {
		t.Fatal("nil shutdown")
	}
	shutdown(context.Background())
}

func TestSetupWithEnvStdoutTrace(t *testing.T) {
	t.Setenv("MOA_TELEMETRY_STDOUT", "true")
	shutdown, err := telemetry.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// When MOA_TELEMETRY_STDOUT=true, StdoutWriter is nil so defaults to os.Stdout
	if shutdown == nil {
		t.Fatal("nil shutdown")
	}
	shutdown(context.Background())
}

func TestSetupWithOTLPEndpoint(t *testing.T) {
	// Point to a non-existent endpoint — the exporter creation should still succeed
	// (errors happen on export, not creation)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:0")
	shutdown, err := telemetry.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if shutdown == nil {
		t.Fatal("nil shutdown")
	}
	shutdown(context.Background())
}

func TestSetupWithOTLPTracesEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:0")
	shutdown, err := telemetry.Setup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	shutdown(context.Background())
}
