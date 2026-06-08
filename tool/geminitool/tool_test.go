package geminitool_test

import (
	"testing"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool/geminitool"
)

func TestNew(t *testing.T) {
	gt := geminitool.New("custom_tool", "A custom native tool", &genai.Tool{GoogleSearch: &genai.GoogleSearch{}})
	if gt.Name() != "custom_tool" {
		t.Fatalf("wrong name: %s", gt.Name())
	}
	if !gt.IsNative() {
		t.Fatal("should be native")
	}
	if gt.IsLongRunning() {
		t.Fatal("should not be long running")
	}
}

func TestGoogleSearch(t *testing.T) {
	gs := geminitool.GoogleSearch()
	if gs.Name() != "google_search" {
		t.Fatal("wrong name")
	}

	req := &model.LLMRequest{}
	if err := gs.ProcessRequest(req); err != nil {
		t.Fatal(err)
	}
	if req.Config == nil || len(req.Config.Tools) == 0 {
		t.Fatal("expected tool injection")
	}
}

func TestCodeExecution(t *testing.T) {
	ce := geminitool.CodeExecution()
	if ce.Name() != "code_execution" {
		t.Fatal("wrong name")
	}

	req := &model.LLMRequest{}
	if err := ce.ProcessRequest(req); err != nil {
		t.Fatal(err)
	}
	if req.Config == nil || len(req.Config.Tools) == 0 {
		t.Fatal("expected tool injection")
	}
}

func TestProcessRequestNilReq(t *testing.T) {
	gs := geminitool.GoogleSearch()
	if err := gs.ProcessRequest(nil); err == nil {
		t.Fatal("expected error for nil req")
	}
}
