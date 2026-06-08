package streaming

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/session"
)

func TestWriteSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	evt := SSEEvent{Event: "chunk", Content: "hello world"}

	writeSSE(rec, rec, evt)

	body := rec.Body.String()
	if !strings.HasPrefix(body, "data: ") {
		t.Errorf("expected SSE data prefix, got %q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("expected double newline suffix, got %q", body)
	}

	// Parse the JSON
	jsonStr := strings.TrimPrefix(strings.TrimSpace(body), "data: ")
	var parsed SSEEvent
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to parse SSE JSON: %v", err)
	}
	if parsed.Event != "chunk" {
		t.Errorf("expected event 'chunk', got %q", parsed.Event)
	}
	if parsed.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %q", parsed.Content)
	}
}

func TestSessionEventToSSE_TextContent(t *testing.T) {
	event := &session.Event{
		Author: "dev-agent",
	}
	event.Content = &genai.Content{
		Parts: []*genai.Part{{Text: "Here's the fix"}},
	}

	sse := sessionEventToSSE(event)
	if sse.Event != "chunk" {
		t.Errorf("expected event 'chunk', got %q", sse.Event)
	}
	if sse.Agent != "dev-agent" {
		t.Errorf("expected agent 'dev-agent', got %q", sse.Agent)
	}
	if sse.Content != "Here's the fix" {
		t.Errorf("expected content, got %q", sse.Content)
	}
}

func TestSessionEventToSSE_ToolCall(t *testing.T) {
	event := &session.Event{}
	event.Content = &genai.Content{
		Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{
				Name: "read_file",
				Args: map[string]any{"path": "main.go"},
			}},
		},
	}

	sse := sessionEventToSSE(event)
	if sse.Event != "tool.call" {
		t.Errorf("expected event 'tool.call', got %q", sse.Event)
	}
	if sse.Tool != "read_file" {
		t.Errorf("expected tool 'read_file', got %q", sse.Tool)
	}
}

func TestSessionEventToSSE_ToolResult(t *testing.T) {
	event := &session.Event{}
	event.Content = &genai.Content{
		Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{
				Name:     "read_file",
				Response: map[string]any{"content": "package main"},
			}},
		},
	}

	sse := sessionEventToSSE(event)
	if sse.Event != "tool.result" {
		t.Errorf("expected event 'tool.result', got %q", sse.Event)
	}
	if sse.Tool != "read_file" {
		t.Errorf("expected tool 'read_file', got %q", sse.Tool)
	}
}

func TestSessionEventToSSE_NilContent(t *testing.T) {
	event := &session.Event{Author: "test"}
	sse := sessionEventToSSE(event)
	if sse.Event != "chunk" {
		t.Errorf("expected event 'chunk', got %q", sse.Event)
	}
	if sse.Content != "" {
		t.Errorf("expected empty content, got %q", sse.Content)
	}
}

func TestParseRequestQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/chat?message=hello&session_id=abc&user_id=bob", nil)
	msg, sid, uid := parseRequest(req)
	if msg != "hello" {
		t.Errorf("expected 'hello', got %q", msg)
	}
	if sid != "abc" {
		t.Errorf("expected 'abc', got %q", sid)
	}
	if uid != "bob" {
		t.Errorf("expected 'bob', got %q", uid)
	}
}

func TestParseRequestJSON(t *testing.T) {
	body := `{"message":"test message","session_id":"s1","user_id":"u1"}`
	req := httptest.NewRequest("POST", "/api/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	msg, sid, uid := parseRequest(req)
	if msg != "test message" {
		t.Errorf("expected 'test message', got %q", msg)
	}
	if sid != "s1" {
		t.Errorf("expected 's1', got %q", sid)
	}
	if uid != "u1" {
		t.Errorf("expected 'u1', got %q", uid)
	}
}

func TestSSEHandlerMissingMessage(t *testing.T) {
	// Test that missing message returns 400
	handler := SSEHandler(nil, Config{})
	req := httptest.NewRequest("GET", "/api/chat", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestWithCORS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	handler := withCORS(inner)

	// OPTIONS preflight
	req := httptest.NewRequest("OPTIONS", "/api/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for OPTIONS, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header")
	}

	// Normal GET
	req = httptest.NewRequest("GET", "/api/chat?message=hi", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header on GET")
	}
}
