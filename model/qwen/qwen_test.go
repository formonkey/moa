package qwen_test

import (
	"testing"

	"github.com/formonkey/moa/model/qwen"
)

func TestNewClient(t *testing.T) {
	c := qwen.NewClient(qwen.Config{APIKey: "test-key"})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Name() == "" {
		t.Fatal("expected non-empty model name")
	}
}
