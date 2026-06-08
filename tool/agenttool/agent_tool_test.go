package agenttool_test

import (
	"context"
	"testing"

	"github.com/formonkey/moa/internal/testutil"
	"github.com/formonkey/moa/tool/agenttool"
)

func TestAgentToolBasic(t *testing.T) {
	child := testutil.MockAgent("helper", "I can help")
	at := agenttool.New(child, nil)

	if at.Name() != "helper" {
		t.Fatalf("wrong name: %s", at.Name())
	}
	if at.Description() == "" {
		t.Fatal("expected description")
	}
	if at.IsNative() {
		t.Fatal("should not be native")
	}
	if at.IsLongRunning() {
		t.Fatal("should not be long running")
	}
	if at.Declaration() == nil {
		t.Fatal("expected declaration")
	}

	result, err := at.Execute(context.Background(), map[string]any{
		"request": "help me",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
}

func TestAgentToolShouldSkipSummarization(t *testing.T) {
	child := testutil.MockAgent("helper", "ok")
	at := agenttool.New(child, nil)

	if iface, ok := at.(interface{ ShouldSkipSummarization() bool }); ok {
		_ = iface.ShouldSkipSummarization()
	}
}

func TestAgentToolMissingRequest(t *testing.T) {
	child := testutil.MockAgent("helper", "ok")
	at := agenttool.New(child, nil)

	result, err := at.Execute(context.Background(), map[string]any{})
	_ = result
	_ = err
}

