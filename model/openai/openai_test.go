package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/model/openai"
)

// mockOpenAIServer creates a test HTTP server that mimics the OpenAI API.
func mockOpenAIServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(handler)
}

func syncResponse(content string, toolCalls ...map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		msg := map[string]any{"role": "assistant", "content": content}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = toolCalls
		}
		resp := map[string]any{
			"id":    "chatcmpl-test",
			"model": "gpt-4o-test",
			"choices": []map[string]any{{
				"index":         0,
				"message":       msg,
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func streamResponse(chunks ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, chunk := range chunks {
			delta := map[string]any{"role": "assistant", "content": chunk}
			resp := map[string]any{
				"choices": []map[string]any{{
					"index": 0,
					"delta": delta,
				}},
			}
			data, _ := json.Marshal(resp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func TestSyncGenerate(t *testing.T) {
	srv := mockOpenAIServer(t, syncResponse("Hello from mock!"))
	defer srv.Close()

	client := openai.NewClient(openai.Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "gpt-4o-test",
	})

	if client.Name() != "gpt-4o-test" {
		t.Fatal("wrong name")
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("hi", "user"),
		},
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
	text := got.Content.Parts[0].Text
	if text != "Hello from mock!" {
		t.Fatalf("wrong text: %q", text)
	}
}

func TestStreamGenerate(t *testing.T) {
	srv := mockOpenAIServer(t, streamResponse("Hello ", "world", "!"))
	defer srv.Close()

	client := openai.NewClient(openai.Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "gpt-4o-test",
	})

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("hi", "user"),
		},
	}

	count := 0
	for _, err := range client.GenerateContent(context.Background(), req, true) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		count++
	}
	if count == 0 {
		t.Fatal("expected stream chunks")
	}
}

func TestWithToolCalls(t *testing.T) {
	toolCall := map[string]any{
		"id":   "call_123",
		"type": "function",
		"function": map[string]any{
			"name":      "get_weather",
			"arguments": `{"city":"Madrid"}`,
		},
	}
	srv := mockOpenAIServer(t, syncResponse("", toolCall))
	defer srv.Close()

	client := openai.NewClient(openai.Config{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "gpt-4o-test",
	})

	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("weather?", "user")},
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "get_weather",
				Parameters: &genai.Schema{
					Type: "OBJECT",
					Properties: map[string]*genai.Schema{
						"city": {Type: "STRING"},
					},
				},
			}},
		}},
	}

	for resp, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
		if resp.Content == nil {
			continue
		}
		for _, p := range resp.Content.Parts {
			if p.FunctionCall != nil && p.FunctionCall.Name == "get_weather" {
				return // Success
			}
		}
	}
	t.Fatal("expected function call")
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	client := openai.NewClient(openai.Config{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user")}}

	for _, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			return // Expected
		}
	}
	t.Fatal("expected error")
}

func TestWithConfig(t *testing.T) {
	srv := mockOpenAIServer(t, syncResponse("ok"))
	defer srv.Close()

	client := openai.NewClient(openai.Config{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	temp := float32(0.5)
	topP := float32(0.9)
	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", "user")},
		Config: &genai.GenerateContentConfig{
			Temperature:    &temp,
			TopP:           &topP,
			MaxOutputTokens: 100,
			StopSequences:   []string{"STOP"},
			ResponseMIMEType: "application/json",
		},
	}

	for _, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}

func TestWithSystemInstruction(t *testing.T) {
	srv := mockOpenAIServer(t, syncResponse("ok"))
	defer srv.Close()

	client := openai.NewClient(openai.Config{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	req := &model.LLMRequest{
		SystemInstruction: &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: "You are helpful"}},
		},
		Contents: []*genai.Content{genai.NewContentFromText("hi", "user")},
	}

	for _, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}

func TestWithCustomHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		syncResponse("ok")(w, r)
	}))
	defer srv.Close()

	client := openai.NewClient(openai.Config{
		APIKey:  "k",
		BaseURL: srv.URL,
		Model:   "m",
		Headers: map[string]string{"X-Custom": "test-value"},
	})

	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", "user")}}
	for range client.GenerateContent(context.Background(), req, false) {
	}
	if gotHeader != "test-value" {
		t.Fatalf("header not sent: %q", gotHeader)
	}
}

func TestFunctionResponseInContents(t *testing.T) {
	srv := mockOpenAIServer(t, syncResponse("result based on tool"))
	defer srv.Close()

	client := openai.NewClient(openai.Config{APIKey: "k", BaseURL: srv.URL, Model: "m"})
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("hi", "user"),
			{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "test", ID: "1", Args: map[string]any{}}}}},
			{Role: "function", Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{Name: "test", ID: "1", Response: map[string]any{"result": "ok"}}}}},
		},
	}

	for _, err := range client.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Fatalf("error: %v", err)
		}
	}
}
