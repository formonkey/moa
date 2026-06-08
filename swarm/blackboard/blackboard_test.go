package blackboard_test

import (
	"sync"
	"testing"

	"github.com/formonkey/moa/swarm/blackboard"
)

func TestWriteAndRead(t *testing.T) {
	bb := blackboard.New()
	bb.Write("key1", "value1")
	bb.Write("key2", 42)

	if got := bb.Read("key1"); got != "value1" {
		t.Fatalf("expected 'value1', got %v", got)
	}
	if got := bb.Read("key2"); got != 42 {
		t.Fatalf("expected 42, got %v", got)
	}
	if got := bb.Read("nonexistent"); got != nil {
		t.Fatalf("expected nil for nonexistent key, got %v", got)
	}
}

func TestDelete(t *testing.T) {
	bb := blackboard.New()
	bb.Write("key", "value")
	bb.Delete("key")

	if got := bb.Read("key"); got != nil {
		t.Fatalf("expected nil after delete, got %v", got)
	}
}

func TestSnapshot(t *testing.T) {
	bb := blackboard.New()
	bb.Write("a", "alpha")
	bb.Write("b", "bravo")

	snap := bb.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries in snapshot, got %d", len(snap))
	}
	if snap["a"] != "alpha" {
		t.Fatalf("expected 'alpha', got %v", snap["a"])
	}

	// Verify snapshot is a deep copy — modifying it shouldn't affect the blackboard
	snap["a"] = "modified"
	if got := bb.Read("a"); got != "alpha" {
		t.Fatal("snapshot modification leaked to blackboard")
	}
}

func TestReviewQueue(t *testing.T) {
	bb := blackboard.New()

	bb.PostForReview("agent-A", "code.go", "review this")
	bb.PostForReview("agent-B", "test.go", "review that")

	if bb.PendingReviews() != 2 {
		t.Fatalf("expected 2 pending reviews, got %d", bb.PendingReviews())
	}

	// Agent-A should not claim their own work
	review := bb.ClaimReview("agent-A")
	if review == nil {
		t.Fatal("expected a review artifact")
	}
	if review.Author != "agent-B" {
		t.Fatalf("expected review from agent-B, got %q", review.Author)
	}
	if review.HandledBy != "agent-A" {
		t.Fatalf("expected HandledBy 'agent-A', got %q", review.HandledBy)
	}

	// Only 1 pending now
	if bb.PendingReviews() != 1 {
		t.Fatalf("expected 1 pending review, got %d", bb.PendingReviews())
	}

	// Agent-B claims agent-A's work
	review2 := bb.ClaimReview("agent-B")
	if review2 == nil {
		t.Fatal("expected a review artifact for agent-B")
	}
	if review2.Author != "agent-A" {
		t.Fatalf("expected review from agent-A, got %q", review2.Author)
	}

	// No more pending
	if bb.PendingReviews() != 0 {
		t.Fatalf("expected 0 pending reviews, got %d", bb.PendingReviews())
	}

	// No unclaimed reviews left
	review3 := bb.ClaimReview("agent-C")
	if review3 != nil {
		t.Fatal("expected nil when no unclaimed reviews exist")
	}
}

func TestConcurrentAccess(t *testing.T) {
	bb := blackboard.New()
	var wg sync.WaitGroup

	// Write concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			bb.Write(string(rune('a'+i%26)), i)
		}(i)
	}
	wg.Wait()

	// Read concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = bb.Read(string(rune('a' + i%26)))
		}(i)
	}
	wg.Wait()

	// Snapshot concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bb.Snapshot()
		}()
	}
	wg.Wait()
}
