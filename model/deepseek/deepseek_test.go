package deepseek_test

import (
	"testing"

	"github.com/formonkey/moa/model/deepseek"
)

func TestNewClient(t *testing.T) {
	c := deepseek.NewClient(deepseek.Config{APIKey: "test-key"})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Name() == "" {
		t.Fatal("expected non-empty model name")
	}
}
