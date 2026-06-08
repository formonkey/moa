package gemini_test

import (
	"context"
	"testing"
	"time"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/model/gemini"
	"google.golang.org/genai"
)

// Gemini adapter requires a real API key for NewClient. We use a dummy key —
// NewClient succeeds but GenerateContent will fail with an auth error.
// All tests that call the API use short timeouts to avoid hanging.

func TestNewClientEmptyModel(t *testing.T) {
	client, err := gemini.NewClient(context.Background(), "test-key-not-real", "")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.Name() != "gemini-2.5-flash" {
		t.Fatalf("expected default model 'gemini-2.5-flash', got %q", client.Name())
	}
}

func TestNewClientCustomModel(t *testing.T) {
	client, err := gemini.NewClient(context.Background(), "test-key", "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.Name() != "gemini-2.0-flash" {
		t.Fatalf("expected 'gemini-2.0-flash', got %q", client.Name())
	}
}

func TestGenerateContentNonStreaming(t *testing.T) {
	client, err := gemini.NewClient(context.Background(), "test-key", "default-model")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	req := &model.LLMRequest{
		Model: "custom-override-model",
		Contents: []*genai.Content{
			genai.NewContentFromText("hello", "user"),
		},
		Config: &genai.GenerateContentConfig{
			Temperature: genai.Ptr(float32(0.5)),
		},
		SystemInstruction: &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: "You are helpful."}},
		},
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{Name: "greet", Description: "Greet someone"},
			},
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for _, err := range client.GenerateContent(ctx, req, false) {
		if err != nil {
			// Expected — API key is invalid
			t.Logf("Expected error from invalid key: %v", err)
			return
		}
	}
}

func TestGenerateContentStreaming(t *testing.T) {
	client, err := gemini.NewClient(context.Background(), "test-key", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("hello", "user"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for _, err := range client.GenerateContent(ctx, req, true) {
		if err != nil {
			t.Logf("Expected error from streaming with invalid key: %v", err)
			return
		}
	}
}

func TestGenerateContentNilConfig(t *testing.T) {
	client, err := gemini.NewClient(context.Background(), "test-key", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("hello", "user"),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for _, err := range client.GenerateContent(ctx, req, false) {
		if err != nil {
			t.Logf("Expected error: %v", err)
			return
		}
	}
}

// Verify interface compliance
var _ model.LLM = (*gemini.Client)(nil)
