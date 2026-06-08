package mistral_test

import (
	"testing"

	"github.com/formonkey/moa/model/mistral"
)

func TestNewClient(t *testing.T) {
	c := mistral.NewClient(mistral.Config{APIKey: "test-key"})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Name() == "" {
		t.Fatal("expected non-empty model name")
	}
}
