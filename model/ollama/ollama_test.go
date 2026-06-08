package ollama_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/model/ollama"
)

func mockOllamaServer(response string, stream bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stream {
			w.Header().Set("Content-Type", "application/x-ndjson")
			// Stream chunks
			for _, chunk := range []string{"Hello ", "world!"} {
				resp := map[string]any{
					"model":   "test",
					"message": map[string]any{"role": "assistant", "content": chunk},
					"done":    false,
				}
				data, _ := json.Marshal(resp)
				w.Write(data)
				w.Write([]byte("\n"))
			}
			// Final chunk
			final := map[string]any{
				"model":   "test",
				"message": map[string]any{"role": "assistant", "content": ""},
				"done":    true,
			}
			data, _ := json.Marshal(final)
			w.Write(data)
			w.Write([]byte("\n"))
		} else {
			resp := map[string]any{
				"model":   "test",
				"message": map[string]any{"role": "assistant", "content": response},
				"done":    true,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}
	}))
}

func TestOllamaSync(t *testing.T) {
	srv := mockOllamaServer("Hello from Ollama!", false)
	defer srv.Close()

	client := ollama.NewClient(srv.URL, "test-model")
	if client.Name() != "test-model" {
		t.Fatal("wrong name")
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", "user")},
	}

	var got *model.LLMResponse
	for resp, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		got = resp
	}
	if got == nil || got.Content == nil {
		t.Fatal("expected response")
	}
}

func TestOllamaStream(t *testing.T) {
	srv := mockOllamaServer("", true)
	defer srv.Close()

	client := ollama.NewClient(srv.URL, "test-model")
	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", "user")},
	}

	count := 0
	for _, err := range client.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected chunks")
	}
}

func TestOllamaWithSystem(t *testing.T) {
	srv := mockOllamaServer("ok", false)
	defer srv.Close()

	client := ollama.NewClient(srv.URL, "m")
	req := &model.LLMRequest{
		SystemInstruction: &genai.Content{
			Role: "system", Parts: []*genai.Part{{Text: "Be helpful"}},
		},
		Contents: []*genai.Content{genai.NewContentFromText("hi", "user")},
	}
	for _, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestOllamaWithTools(t *testing.T) {
	srv := mockOllamaServer("ok", false)
	defer srv.Close()

	client := ollama.NewClient(srv.URL, "m")
	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", "user")},
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name:       "test",
				Parameters: &genai.Schema{Type: "OBJECT"},
			}},
		}},
	}
	for _, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestOllamaDefaults(t *testing.T) {
	c := ollama.NewClient("", "")
	if c.Name() == "" {
		t.Fatal("expected default model name")
	}
}

func TestOllamaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	client := ollama.NewClient(srv.URL, "m")
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user")}}

	for _, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			return
		}
	}
	t.Fatal("expected error")
}
