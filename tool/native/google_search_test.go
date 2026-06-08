package native_test

import (
	"testing"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool/native"
)

func TestGoogleSearch(t *testing.T) {
	gs := &native.GoogleSearch{}

	if gs.Name() != "google_search" {
		t.Fatal("wrong name")
	}
	if gs.Description() == "" {
		t.Fatal("expected non-empty description")
	}
	if !gs.IsNative() {
		t.Fatal("should be native")
	}
	if gs.IsLongRunning() {
		t.Fatal("should not be long running")
	}

	req := &model.LLMRequest{}
	if err := gs.ProcessRequest(req); err != nil {
		t.Fatalf("ProcessRequest: %v", err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	if req.Tools[0].GoogleSearch == nil {
		t.Fatal("expected GoogleSearch tool")
	}
}
