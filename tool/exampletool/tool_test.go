package exampletool_test

import (
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool/exampletool"
)

func TestExampleToolProcessRequest(t *testing.T) {
	et := exampletool.New(exampletool.Config{
		Examples: []*exampletool.Example{
			{
				Input:  &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "hello"}}},
				Output: []*genai.Content{{Role: "model", Parts: []*genai.Part{{Text: "Hi there!"}}}},
			},
			{
				Input:  &genai.Content{Role: "user", Parts: []*genai.Part{{Text: "bye"}}},
				Output: []*genai.Content{{Role: "model", Parts: []*genai.Part{{Text: "Goodbye!"}}}},
			},
		},
	})

	if et.Name() == "" {
		t.Fatal("expected name")
	}
	if et.Description() == "" {
		t.Fatal("expected description")
	}
	if et.IsLongRunning() {
		t.Fatal("should not be long running")
	}

	// ProcessRequest should inject examples into request
	req := &model.LLMRequest{}
	if nt, ok := et.(interface{ ProcessRequest(*model.LLMRequest) }); ok {
		nt.ProcessRequest(req)
		if req.SystemInstruction == nil {
			t.Fatal("expected system instruction with examples")
		}
	} else {
		t.Fatal("expected ProcessRequest method")
	}
}

func TestExampleToolEmpty(t *testing.T) {
	et := exampletool.New(exampletool.Config{})
	if et.Name() == "" {
		t.Fatal("expected name")
	}
	// ProcessRequest with no examples should be no-op
	req := &model.LLMRequest{}
	if nt, ok := et.(interface{ ProcessRequest(*model.LLMRequest) }); ok {
		nt.ProcessRequest(req)
	}
}
