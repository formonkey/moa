package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/model/anthropic"
)

func mockAnthropicSync(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "msg_test",
			"type":  "message",
			"model": "claude-test",
			"content": []map[string]any{
				{"type": "text", "text": content},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func mockAnthropicStream(chunks ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// message_start
		start := map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_test", "model": "claude-test", "usage": map[string]any{"input_tokens": 10}}}
		data, _ := json.Marshal(start)
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", data)
		if flusher != nil { flusher.Flush() }

		// content blocks
		blockStart := map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}}
		data, _ = json.Marshal(blockStart)
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", data)
		if flusher != nil { flusher.Flush() }

		for _, chunk := range chunks {
			delta := map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": chunk}}
			data, _ = json.Marshal(delta)
			fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", data)
			if flusher != nil { flusher.Flush() }
		}

		blockStop := map[string]any{"type": "content_block_stop", "index": 0}
		data, _ = json.Marshal(blockStop)
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", data)
		if flusher != nil { flusher.Flush() }

		msgStop := map[string]any{"type": "message_stop"}
		data, _ = json.Marshal(msgStop)
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", data)
		if flusher != nil { flusher.Flush() }
	}
}

func TestAnthropicSync(t *testing.T) {
	srv := httptest.NewServer(mockAnthropicSync("Hello from Claude!"))
	defer srv.Close()

	client := anthropic.NewClient(anthropic.Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "claude-test",
	})

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

func TestAnthropicStream(t *testing.T) {
	srv := httptest.NewServer(mockAnthropicStream("Hello ", "world!"))
	defer srv.Close()

	client := anthropic.NewClient(anthropic.Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})

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

func TestAnthropicError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	client := anthropic.NewClient(anthropic.Config{APIKey: "k", BaseURL: srv.URL})
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user")}}

	for _, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			return
		}
	}
	t.Fatal("expected error")
}

func TestAnthropicWithSystem(t *testing.T) {
	srv := httptest.NewServer(mockAnthropicSync("ok"))
	defer srv.Close()

	client := anthropic.NewClient(anthropic.Config{APIKey: "k", BaseURL: srv.URL})
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

func TestAnthropicWithTools(t *testing.T) {
	srv := httptest.NewServer(mockAnthropicSync("ok"))
	defer srv.Close()

	client := anthropic.NewClient(anthropic.Config{APIKey: "k", BaseURL: srv.URL})
	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", "user")},
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "test_tool",
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

func TestAnthropicDefaults(t *testing.T) {
	c := anthropic.NewClient(anthropic.Config{APIKey: "k"})
	if c.Name() == "" {
		t.Fatal("expected default model name")
	}
}
