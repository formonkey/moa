// Package testutil provides shared mocks and helpers for MOA tests.
package testutil

import (
	"context"
	"iter"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/session"
)

// MockModel is a configurable mock LLM model.
type MockModel struct {
	ModelName      string
	Response       string
	FunctionCalls  []*genai.FunctionCall
	StreamChunks   []string
	Err            error
	CallCount      int
}

func (m *MockModel) Name() string { return m.ModelName }

func (m *MockModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.CallCount++

		if m.Err != nil {
			yield(nil, m.Err)
			return
		}

		// Stream mode
		if len(m.StreamChunks) > 0 {
			for i, chunk := range m.StreamChunks {
				isLast := i == len(m.StreamChunks)-1
				if !yield(&model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: chunk}},
					},
					ModelVersion: m.ModelName,
					Partial:      !isLast,
				}, nil) {
					return
				}
			}
			return
		}

		// Function call mode
		if len(m.FunctionCalls) > 0 {
			parts := make([]*genai.Part, len(m.FunctionCalls))
			for i, fc := range m.FunctionCalls {
				parts[i] = &genai.Part{FunctionCall: fc}
			}
			yield(&model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: parts,
				},
				ModelVersion: m.ModelName,
			}, nil)
			m.FunctionCalls = nil
			return
		}

		// Simple text mode
		resp := m.Response
		if resp == "" {
			resp = "mock response"
		}
		yield(&model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: resp}},
			},
			ModelVersion:  m.ModelName,
			FinishReason:  "STOP",
		}, nil)
	}
}

// MockAgent creates a simple agent that returns a fixed text.
func MockAgent(name, response string) agent.Agent {
	a, _ := agent.New(agent.Config{
		Name:        name,
		Description: name + " mock",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				evt := session.NewEvent(ctx.InvocationID())
				evt.Author = name
				evt.LLMResponse = model.LLMResponse{
					Content: &genai.Content{
						Role:  "model",
						Parts: []*genai.Part{{Text: response}},
					},
				}
				yield(evt, nil)
			}
		},
	})
	return a
}

// MockErrorAgent creates an agent that always returns an error.
func MockErrorAgent(name string, err error) agent.Agent {
	a, _ := agent.New(agent.Config{
		Name: name,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(nil, err)
			}
		},
	})
	return a
}

// NewTestSession creates a test session using InMemoryService.
func NewTestSession(t interface{ Helper(); Fatalf(string, ...any) }) (session.Service, session.Session) {
	t.Helper()
	svc := session.InMemoryService()
	ctx := context.Background()
	resp, err := svc.Create(ctx, &session.CreateRequest{
		AppName:   "test",
		UserID:    "test-user",
		SessionID: "test-session",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return svc, resp.Session
}

// NewTestContext creates a test InvocationContext.
func NewTestContext(a agent.Agent, sess session.Session) agent.InvocationContext {
	return agent.NewInvocationContext(agent.InvocationContextParams{
		Ctx:          context.Background(),
		Agent:        a,
		Session:      sess,
		InvocationID: "test-inv",
		UserContent: &genai.Content{
			Role:  "user",
			Parts: []*genai.Part{{Text: "test input"}},
		},
	})
}
