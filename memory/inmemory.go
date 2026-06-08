package memory

import (
	"context"
	"maps"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/session"
)

// InMemoryService returns a thread-safe in-memory implementation of the memory Service.
// Uses simple keyword matching for search (same approach as ADK Go).
func InMemoryService() Service {
	return &inMemoryService{
		store: make(map[memKey]map[string][]memValue),
	}
}

type memKey struct {
	appName, userID string
}

type memValue struct {
	id             string
	content        *genai.Content
	author         string
	timestamp      time.Time
	customMetadata map[string]any
	words          map[string]struct{} // precomputed for search
}

type inMemoryService struct {
	mu    sync.RWMutex
	store map[memKey]map[string][]memValue // key -> sessionID -> values
}

func (s *inMemoryService) AddSessionToMemory(_ context.Context, sess session.Session) error {
	var values []memValue

	for event := range sess.Events().All() {
		if event.Content == nil {
			continue
		}

		words := make(map[string]struct{})
		for _, part := range event.Content.Parts {
			if part.Text == "" {
				continue
			}
			maps.Copy(words, extractWords(part.Text))
		}

		if len(words) == 0 {
			continue
		}

		values = append(values, memValue{
			id:             event.ID,
			content:        event.Content,
			author:         event.Author,
			timestamp:      event.Timestamp,
			customMetadata: event.CustomMetadata,
			words:          words,
		})
	}

	k := memKey{
		appName: sess.AppName(),
		userID:  sess.UserID(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.store[k] == nil {
		s.store[k] = make(map[string][]memValue)
	}
	s.store[k][sess.ID()] = values
	return nil
}

func (s *inMemoryService) SearchMemory(_ context.Context, req *SearchRequest) (*SearchResponse, error) {
	queryWords := extractWords(req.Query)

	k := memKey{
		appName: req.AppName,
		userID:  req.UserID,
	}

	s.mu.RLock()
	sessions, ok := s.store[k]
	s.mu.RUnlock()
	if !ok {
		return &SearchResponse{}, nil
	}

	res := &SearchResponse{}
	for _, events := range sessions {
		for _, e := range events {
			if checkIntersect(e.words, queryWords) {
				res.Memories = append(res.Memories, Entry{
					ID:             e.id,
					Content:        e.content,
					Author:         e.author,
					Timestamp:      e.timestamp,
					CustomMetadata: e.customMetadata,
				})
			}
		}
	}

	return res, nil
}

func checkIntersect(m1, m2 map[string]struct{}) bool {
	if len(m1) == 0 || len(m2) == 0 {
		return false
	}
	if len(m1) > len(m2) {
		m1, m2 = m2, m1
	}
	for k := range m1 {
		if _, ok := m2[k]; ok {
			return true
		}
	}
	return false
}

func extractWords(text string) map[string]struct{} {
	res := make(map[string]struct{})
	for _, w := range strings.Fields(text) {
		if w != "" {
			res[strings.ToLower(w)] = struct{}{}
		}
	}
	return res
}

var _ Service = (*inMemoryService)(nil)
