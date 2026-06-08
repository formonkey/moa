package groq_test

import (
	"testing"

	"github.com/formonkey/moa/model/groq"
)

func TestNewClient(t *testing.T) {
	c := groq.NewClient(groq.Config{APIKey: "test-key"})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Name() == "" {
		t.Fatal("expected non-empty model name")
	}
}
