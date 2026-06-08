package session_test

import (
	"testing"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
	"google.golang.org/genai"
)

func TestNewEvent(t *testing.T) {
	e := session.NewEvent("inv-1")
	if e.ID == "" {
		t.Fatal("expected non-empty event ID")
	}
	if e.InvocationID != "inv-1" {
		t.Fatalf("expected invocation ID 'inv-1', got %q", e.InvocationID)
	}
	if e.Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}
}

func TestIsFinalResponse(t *testing.T) {
	tests := []struct {
		name     string
		event    *session.Event
		expected bool
	}{
		{
			name: "nil content = still final (no pending calls)",
			event: &session.Event{
				LLMResponse: model.LLMResponse{Content: nil},
			},
			expected: true,
		},
		{
			name: "text only = final",
			event: &session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: "Hello!"}},
					},
				},
			},
			expected: true,
		},
		{
			name: "function call = not final",
			event: &session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Role: "model",
						Parts: []*genai.Part{{
							FunctionCall: &genai.FunctionCall{Name: "test"},
						}},
					},
				},
			},
			expected: false,
		},
		{
			name: "function response = not final",
			event: &session.Event{
				LLMResponse: model.LLMResponse{
					Content: &genai.Content{
						Role: "model",
						Parts: []*genai.Part{{
							FunctionResponse: &genai.FunctionResponse{Name: "test"},
						}},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.event.IsFinalResponse()
			if got != tt.expected {
				t.Errorf("IsFinalResponse() = %v, want %v", got, tt.expected)
			}
		})
	}
}
