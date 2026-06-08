// Package blackboard implements a shared, concurrent data matrix for multi-agent coordination.
//
// It acts as a stigmergic whiteboard where agents post findings, constraints,
// or completed subtasks for others to read asynchronously. The Blackboard also
// provides a peer-review queue where idle agents can claim and audit peers' work.
package blackboard

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Blackboard implements a shared, concurrent data matrix for multi-agent validation.
type Blackboard struct {
	board       sync.Map
	reviews     []ReviewArtifact
	reviewMutex sync.Mutex
}

// ReviewArtifact represents a piece of generated work waiting for peer validation.
type ReviewArtifact struct {
	ID        string
	Author    string
	Asset     string
	Context   string
	HandledBy string
}

// New creates a fresh Blackboard instance.
func New() *Blackboard {
	return &Blackboard{}
}

// Write posts a value onto the shared matrix. Overwrites if key exists.
func (bb *Blackboard) Write(key string, value any) {
	bb.board.Store(key, value)
}

// Read extracts a value from the matrix. Returns nil if not found.
func (bb *Blackboard) Read(key string) any {
	if val, ok := bb.board.Load(key); ok {
		return val
	}
	return nil
}

// Delete removes a key from the Blackboard.
func (bb *Blackboard) Delete(key string) {
	bb.board.Delete(key)
}

// Snapshot returns a deep-copied point-in-time view of the entire Blackboard state.
// Safe for injection into agent context without pointer leaks.
func (bb *Blackboard) Snapshot() map[string]any {
	snap := make(map[string]any)
	bb.board.Range(func(key, value any) bool {
		strKey, ok := key.(string)
		if !ok {
			return true
		}
		// Deep copy via JSON round-trip to prevent pointer leaks
		b, err := json.Marshal(value)
		if err == nil {
			var deepCopied any
			if json.Unmarshal(b, &deepCopied) == nil {
				snap[strKey] = deepCopied
			} else {
				snap[strKey] = value // fallback to shallow
			}
		} else {
			snap[strKey] = value // fallback for non-serializable types
		}
		return true
	})
	return snap
}

// PostForReview pushes an artifact into the Blackboard's audit queue.
func (bb *Blackboard) PostForReview(author, asset, context string) {
	bb.reviewMutex.Lock()
	defer bb.reviewMutex.Unlock()
	bb.reviews = append(bb.reviews, ReviewArtifact{
		ID:      fmt.Sprintf("rev-%d", time.Now().UnixNano()),
		Author:  author,
		Asset:   asset,
		Context: context,
	})
}

// ClaimReview lets an idle agent take ownership of a pending peer audit.
// Returns nil if no unclaimed reviews exist (or all are by the same author).
func (bb *Blackboard) ClaimReview(reviewer string) *ReviewArtifact {
	bb.reviewMutex.Lock()
	defer bb.reviewMutex.Unlock()
	for i, rev := range bb.reviews {
		if rev.HandledBy == "" && rev.Author != reviewer {
			bb.reviews[i].HandledBy = reviewer
			return &bb.reviews[i]
		}
	}
	return nil
}

// PendingReviews returns the count of unclaimed review artifacts.
func (bb *Blackboard) PendingReviews() int {
	bb.reviewMutex.Lock()
	defer bb.reviewMutex.Unlock()
	count := 0
	for _, rev := range bb.reviews {
		if rev.HandledBy == "" {
			count++
		}
	}
	return count
}
