package functioncallmodifier_test

import (
	"context"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/plugin/functioncallmodifier"
	"github.com/formonkey/moa/session"
)

type mockCbCtx struct {
	context.Context
	sess session.Session
}

func (m *mockCbCtx) UserContent() *genai.Content            { return nil }
func (m *mockCbCtx) InvocationID() string                    { return "inv-1" }
func (m *mockCbCtx) AgentName() string                       { return "agent" }
func (m *mockCbCtx) ReadonlyState() session.ReadonlyState    { return m.sess.State() }
func (m *mockCbCtx) UserID() string                          { return "u1" }
func (m *mockCbCtx) AppName() string                         { return "app" }
func (m *mockCbCtx) SessionID() string                       { return "s1" }
func (m *mockCbCtx) Branch() string                          { return "" }
func (m *mockCbCtx) Artifacts() agent.Artifacts              { return nil }
func (m *mockCbCtx) State() session.State                    { return m.sess.State() }

var _ agent.CallbackContext = (*mockCbCtx)(nil)

func newCbCtx() *mockCbCtx {
	svc := session.InMemoryService()
	resp, _ := svc.Create(context.Background(), &session.CreateRequest{AppName: "test", UserID: "u1", SessionID: "s1"})
	return &mockCbCtx{Context: context.Background(), sess: resp.Session}
}

func TestFunctionCallModifierBeforeModel(t *testing.T) {
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return name == "target_tool" },
		Args: map[string]*genai.Schema{
			"extra_param": {Type: "STRING", Description: "injected param"},
		},
		OverrideDescription: func(original string) string {
			return "Modified: " + original
		},
	})

	if p.Name != "function-call-modifier" {
		t.Fatal("wrong name")
	}

	ctx := newCbCtx()

	req := &model.LLMRequest{
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{Name: "target_tool", Description: "Original desc", Parameters: &genai.Schema{
					Type:       "OBJECT",
					Properties: map[string]*genai.Schema{"input": {Type: "STRING"}},
				}},
				{Name: "other_tool", Description: "Not modified"},
			},
		}},
	}

	resp, err := p.BeforeModelCallback(ctx, req)
	if err != nil {
		t.Fatalf("BeforeModelCallback: %v", err)
	}
	if resp != nil {
		t.Fatal("expected nil response")
	}

	decl := req.Tools[0].FunctionDeclarations[0]
	if decl.Parameters.Properties["extra_param"] == nil {
		t.Fatal("expected injected param")
	}
	if decl.Description != "Modified: Original desc" {
		t.Fatalf("expected modified description, got %q", decl.Description)
	}

	// Verify other_tool was NOT modified
	otherDecl := req.Tools[0].FunctionDeclarations[1]
	if otherDecl.Description != "Not modified" {
		t.Fatal("other_tool should not be modified")
	}
}

func TestFunctionCallModifierNilParams(t *testing.T) {
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return true },
		Args:      map[string]*genai.Schema{"p": {Type: "STRING"}},
	})

	ctx := newCbCtx()

	req := &model.LLMRequest{
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{Name: "tool_no_params"},
			},
		}},
	}

	_, err := p.BeforeModelCallback(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	// Should have created Parameters with the injected param
	decl := req.Tools[0].FunctionDeclarations[0]
	if decl.Parameters == nil {
		t.Fatal("expected Parameters to be created")
	}
	if decl.Parameters.Properties["p"] == nil {
		t.Fatal("expected injected param 'p'")
	}
}

func TestFunctionCallModifierNilProperties(t *testing.T) {
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return true },
		Args:      map[string]*genai.Schema{"q": {Type: "STRING"}},
	})

	ctx := newCbCtx()

	req := &model.LLMRequest{
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{Name: "tool_with_schema", Parameters: &genai.Schema{Type: "OBJECT"}}, // Properties is nil
			},
		}},
	}

	_, err := p.BeforeModelCallback(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	decl := req.Tools[0].FunctionDeclarations[0]
	if decl.Parameters.Properties["q"] == nil {
		t.Fatal("expected injected param 'q'")
	}
}

func TestFunctionCallModifierNilFunctionDeclarations(t *testing.T) {
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return true },
		Args:      map[string]*genai.Schema{"x": {Type: "STRING"}},
	})

	ctx := newCbCtx()

	req := &model.LLMRequest{
		Tools: []*genai.Tool{
			{}, // No FunctionDeclarations
		},
	}

	_, err := p.BeforeModelCallback(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic
}

func TestFunctionCallModifierAfterModel(t *testing.T) {
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return name == "write_file" },
		Args: map[string]*genai.Schema{
			"confidence": {Type: "NUMBER", Description: "how confident"},
		},
	})

	ctx := newCbCtx()

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{
					ID:   "fc-1",
					Name: "write_file",
					Args: map[string]any{
						"path":       "/tmp/test.go",
						"content":    "package main",
						"confidence": 0.95,
					},
				}},
			},
		},
	}

	result, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatalf("AfterModelCallback: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result")
	}

	// Verify confidence was removed from args
	fc := resp.Content.Parts[0].FunctionCall
	if _, ok := fc.Args["confidence"]; ok {
		t.Fatal("expected 'confidence' to be removed from function call args")
	}

	// Verify original args are intact
	if fc.Args["path"] != "/tmp/test.go" {
		t.Fatal("expected 'path' to remain")
	}

	// Verify confidence was saved to state
	stateKey := "fc-1/confidence"
	val, _ := ctx.State().Get(stateKey)
	if val == nil {
		t.Fatal("expected confidence saved to state")
	}
}

func TestFunctionCallModifierAfterModelNilResponse(t *testing.T) {
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return true },
		Args:      map[string]*genai.Schema{"x": {Type: "STRING"}},
	})

	ctx := newCbCtx()

	// nil response
	result, err := p.AfterModelCallback(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil")
	}

	// nil content
	result, err = p.AfterModelCallback(ctx, &model.LLMResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil")
	}
}

func TestFunctionCallModifierAfterModelNoMatch(t *testing.T) {
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return name == "target" },
		Args:      map[string]*genai.Schema{"x": {Type: "STRING"}},
	})

	ctx := newCbCtx()

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{
					ID:   "fc-2",
					Name: "other_tool", // doesn't match predicate
					Args: map[string]any{"x": "val"},
				}},
			},
		},
	}

	_, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatal(err)
	}

	// x should NOT have been removed
	fc := resp.Content.Parts[0].FunctionCall
	if _, ok := fc.Args["x"]; !ok {
		t.Fatal("expected 'x' to remain since predicate didn't match")
	}
}

func TestFunctionCallModifierAfterModelMissingArg(t *testing.T) {
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return true },
		Args:      map[string]*genai.Schema{"missing_param": {Type: "STRING"}},
	})

	ctx := newCbCtx()

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{
					ID:   "fc-3",
					Name: "tool",
					Args: map[string]any{"other": "value"}, // missing_param not present
				}},
			},
		},
	}

	_, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic — just skip missing args
}

func TestFunctionCallModifierNoOverrideDescription(t *testing.T) {
	// OverrideDescription is nil — should not modify description
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return true },
		Args:      map[string]*genai.Schema{"p": {Type: "STRING"}},
		// OverrideDescription is nil
	})

	ctx := newCbCtx()

	req := &model.LLMRequest{
		Tools: []*genai.Tool{{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{Name: "tool", Description: "Original"},
			},
		}},
	}

	_, err := p.BeforeModelCallback(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if req.Tools[0].FunctionDeclarations[0].Description != "Original" {
		t.Fatal("description should not be modified when OverrideDescription is nil")
	}
}

func TestFunctionCallModifierTextPart(t *testing.T) {
	// Response with text part (not function call) should be handled gracefully
	p := functioncallmodifier.New(functioncallmodifier.Config{
		Predicate: func(name string) bool { return true },
		Args:      map[string]*genai.Schema{"p": {Type: "STRING"}},
	})

	ctx := newCbCtx()

	resp := &model.LLMResponse{
		Content: &genai.Content{
			Role: "model",
			Parts: []*genai.Part{
				{Text: "Just a text response"}, // No FunctionCall
			},
		},
	}

	_, err := p.AfterModelCallback(ctx, resp)
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic
}
