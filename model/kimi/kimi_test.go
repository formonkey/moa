package kimi_test

import (
	"testing"

	"github.com/formonkey/moa/model/kimi"
)

func TestNewClient(t *testing.T) {
	c := kimi.NewClient(kimi.Config{APIKey: "test-key"})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Name() == "" {
		t.Fatal("expected non-empty model name")
	}
}
