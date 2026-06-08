package badger_test

import (
	"testing"

	"github.com/formonkey/moa/memory"
	"github.com/formonkey/moa/memory/badger"
)

func TestBadgerStore(t *testing.T) {
	dir := t.TempDir()
	store := badger.NewStore(dir)
	if store == nil {
		t.Fatal("expected non-nil store")
	}

	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer store.Close()

	// Add a message
	err := store.AddMessage(memory.Message{
		SessionID: "s1",
		Role:      "user",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("AddMessage: %v", err)
	}

	// Retrieve history
	history, err := store.GetSessionHistory("s1")
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("expected messages in history")
	}
}
