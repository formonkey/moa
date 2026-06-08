package openrouter_test

import (
	"testing"

	"github.com/formonkey/moa/model/openrouter"
)

func TestNewClient(t *testing.T) {
	c := openrouter.NewClient(openrouter.Config{
		APIKey:  "test-key",
		Model:   "anthropic/claude-sonnet-4",
		AppName: "test-app",
	})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Name() != "anthropic/claude-sonnet-4" {
		t.Fatalf("wrong name: %s", c.Name())
	}
}

func TestNewClientWithHeaders(t *testing.T) {
	c := openrouter.NewClient(openrouter.Config{
		APIKey:  "test-key",
		Model:   "meta-llama/llama-3.3-70b",
		AppName: "my-app",
		AppURL:  "https://example.com",
	})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}
