// Package vector provides a VectorStore-backed memory implementation.
//
// Users bring their own vector database by implementing the Store interface.
// This keeps go-brain zero-dependency while supporting any vector DB
// (Qdrant, Chroma, Pinecone, Milvus, pgvector, etc.).
package vector

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/genai"

	"github.com/formonkey/moa/memory"
	"github.com/formonkey/moa/session"
)

// Document represents a chunk of text stored in the vector database.
type Document struct {
	ID       string
	Content  string
	Source   string         // origin file or agent name
	Metadata map[string]any // arbitrary metadata
}

// Store is the interface users implement to plug in their vector database.
type Store interface {
	// Save stores a document. Implementations handle embedding generation.
	Save(ctx context.Context, doc Document) error
	// Search returns the top-K most relevant documents for a query.
	Search(ctx context.Context, query string, topK int) ([]Document, error)
	// DeleteBySource removes all documents from a given source.
	DeleteBySource(ctx context.Context, source string) error
}

// Memory wraps a user-provided VectorStore as a go-brain memory.Service.
type Memory struct {
	mu    sync.RWMutex
	store Store
	topK  int
}

// New creates a VectorMemory backed by the provided Store.
func New(store Store, topK int) *Memory {
	if topK <= 0 {
		topK = 10
	}
	return &Memory{store: store, topK: topK}
}

// AddSessionToMemory ingests all events from a session into the vector store.
func (v *Memory) AddSessionToMemory(ctx context.Context, s session.Session) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	for event := range s.Events().All() {
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.Text == "" {
				continue
			}
			doc := Document{
				ID:      event.ID,
				Content: part.Text,
				Source:  fmt.Sprintf("session:%s", s.ID()),
				Metadata: map[string]any{
					"author":    event.Author,
					"timestamp": event.Timestamp.String(),
				},
			}
			if err := v.store.Save(ctx, doc); err != nil {
				return fmt.Errorf("vector: failed to save event %s: %w", event.ID, err)
			}
		}
	}
	return nil
}

// SearchMemory returns documents relevant to the query.
func (v *Memory) SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	docs, err := v.store.Search(ctx, req.Query, v.topK)
	if err != nil {
		return nil, fmt.Errorf("vector: search failed: %w", err)
	}

	entries := make([]memory.Entry, 0, len(docs))
	for _, doc := range docs {
		entries = append(entries, memory.Entry{
			ID: doc.ID,
			Content: &genai.Content{
				Role:  "user",
				Parts: []*genai.Part{{Text: doc.Content}},
			},
			Author:         doc.Source,
			CustomMetadata: doc.Metadata,
		})
	}

	return &memory.SearchResponse{Memories: entries}, nil
}

// IngestFile reads content and stores it as a document under the given source name.
func (v *Memory) IngestFile(ctx context.Context, source, content string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	doc := Document{
		ID:      fmt.Sprintf("file:%s", source),
		Content: fmt.Sprintf("FILE: %s\n```\n%s\n```", source, content),
		Source:  source,
		Metadata: map[string]any{
			"type": "file",
		},
	}
	return v.store.Save(ctx, doc)
}

var _ memory.Service = (*Memory)(nil)
