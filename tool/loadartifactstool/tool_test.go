package loadartifactstool_test

import (
	"context"
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/loadartifactstool"
)

type mockArtifactLoader struct{}

func (m *mockArtifactLoader) ListArtifacts(ctx context.Context) ([]string, error) {
	return []string{"doc1.md", "doc2.md"}, nil
}

func (m *mockArtifactLoader) LoadArtifact(ctx context.Context, name string) (*genai.Part, error) {
	return &genai.Part{Text: "artifact content: " + name}, nil
}

func TestLoadArtifactsToolAccessors(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	if lat.Name() != "load_artifacts" {
		t.Fatalf("wrong name: %s", lat.Name())
	}
	if lat.Description() == "" {
		t.Fatal("empty description")
	}
	if lat.IsNative() {
		t.Fatal("not native")
	}
	if lat.IsLongRunning() {
		t.Fatal("not long running")
	}
}

func TestLoadArtifactsToolDeclaration(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	if rt, ok := lat.(tool.RunnableTool); ok {
		decl := rt.Declaration()
		if decl == nil {
			t.Fatal("nil declaration")
		}
		if decl.Name != "load_artifacts" {
			t.Fatal("wrong declaration name")
		}
	} else {
		t.Fatal("expected RunnableTool")
	}
}

func TestLoadArtifactsToolExecute(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	rt := lat.(tool.RunnableTool)

	// With artifact_names
	result, err := rt.Execute(context.Background(), map[string]any{
		"artifact_names": []interface{}{"doc1.md"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
}

func TestLoadArtifactsToolExecuteNoNames(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	rt := lat.(tool.RunnableTool)

	result, err := rt.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
}

func TestLoadArtifactsToolProcessRequest(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	if pr, ok := lat.(interface {
		ProcessRequest(context.Context, *model.LLMRequest) error
	}); ok {
		req := &model.LLMRequest{}
		err := pr.ProcessRequest(context.Background(), req)
		if err != nil {
			t.Fatalf("ProcessRequest: %v", err)
		}
		if req.SystemInstruction == nil {
			t.Fatal("expected system instruction with artifact list")
		}
	} else {
		t.Fatal("expected ProcessRequest")
	}
}

func TestLoadArtifactsProcessRequestWithFunctionResponse(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	pr := lat.(interface {
		ProcessRequest(context.Context, *model.LLMRequest) error
	})

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "function",
				Parts: []*genai.Part{{
					FunctionResponse: &genai.FunctionResponse{
						Name:     "load_artifacts",
						Response: map[string]any{"artifact_names": []interface{}{"doc1.md"}},
					},
				}},
			},
		},
	}

	err := pr.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("ProcessRequest with fn response: %v", err)
	}
	// Should have appended artifact content
	if len(req.Contents) <= 1 {
		t.Fatal("expected artifact content appended")
	}
}

func TestLoadArtifactsProcessRequestEmpty(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	pr := lat.(interface {
		ProcessRequest(context.Context, *model.LLMRequest) error
	})

	req := &model.LLMRequest{Contents: []*genai.Content{}}
	err := pr.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAppendInstructionExisting(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	pr := lat.(interface {
		ProcessRequest(context.Context, *model.LLMRequest) error
	})

	// Pre-existing system instruction
	req := &model.LLMRequest{
		SystemInstruction: &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: "Existing instruction."}},
		},
	}
	err := pr.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	// Should have appended, not replaced
	text := req.SystemInstruction.Parts[0].Text
	if len(text) <= len("Existing instruction.") {
		t.Fatal("expected instruction to grow")
	}
}

func TestProcessRequestNilLoader(t *testing.T) {
	lat := loadartifactstool.New(nil)
	pr := lat.(interface {
		ProcessRequest(context.Context, *model.LLMRequest) error
	})

	req := &model.LLMRequest{}
	err := pr.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("nil loader should not error: %v", err)
	}
}

func TestProcessRequestNonLoadArtifactsResponse(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	pr := lat.(interface {
		ProcessRequest(context.Context, *model.LLMRequest) error
	})

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "function",
				Parts: []*genai.Part{{
					FunctionResponse: &genai.FunctionResponse{
						Name:     "other_tool",
						Response: map[string]any{"result": "ok"},
					},
				}},
			},
		},
	}
	err := pr.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("non-load_artifacts should not error: %v", err)
	}
}

func TestProcessRequestNilContentParts(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	pr := lat.(interface {
		ProcessRequest(context.Context, *model.LLMRequest) error
	})

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{Role: "user", Parts: nil},
		},
	}
	err := pr.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("nil parts should not error: %v", err)
	}
}

func TestProcessRequestEmptyArtifactNames(t *testing.T) {
	lat := loadartifactstool.New(&mockArtifactLoader{})
	pr := lat.(interface {
		ProcessRequest(context.Context, *model.LLMRequest) error
	})

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "function",
				Parts: []*genai.Part{{
					FunctionResponse: &genai.FunctionResponse{
						Name:     "load_artifacts",
						Response: map[string]any{"artifact_names": []interface{}{}},
					},
				}},
			},
		},
	}
	err := pr.ProcessRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("empty names should not error: %v", err)
	}
}
