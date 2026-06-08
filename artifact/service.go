// Package artifact provides versioned artifact storage for go-brain.
//
// Artifacts are binary blobs (files, images, documents) that can be produced
// and consumed by agents during a session. Each artifact is versioned — every
// save creates a new version, and any version can be loaded.
package artifact

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/genai"
)

// Service is the interface for versioned artifact storage.
type Service interface {
	// Save stores a new version of an artifact. Returns the new version number.
	Save(ctx context.Context, req *SaveRequest) (int64, error)
	// Load retrieves the latest version of an artifact.
	Load(ctx context.Context, req *LoadRequest) (*LoadResponse, error)
	// List returns the names of all artifacts in a session.
	List(ctx context.Context, req *ListRequest) ([]string, error)
	// Delete removes all versions of an artifact.
	Delete(ctx context.Context, req *DeleteRequest) error
	// Versions returns the number of versions of an artifact.
	Versions(ctx context.Context, req *VersionsRequest) (int64, error)
}

// SaveRequest is a request to save an artifact.
type SaveRequest struct {
	AppName   string
	UserID    string
	SessionID string
	Filename  string
	Data      *genai.Part
}

// LoadRequest is a request to load an artifact.
type LoadRequest struct {
	AppName   string
	UserID    string
	SessionID string
	Filename  string
	// Version to load. 0 means latest.
	Version int64
}

// LoadResponse is the response from loading an artifact.
type LoadResponse struct {
	Data    *genai.Part
	Version int64
}

// ListRequest is a request to list artifact filenames.
type ListRequest struct {
	AppName   string
	UserID    string
	SessionID string
}

// DeleteRequest is a request to delete an artifact.
type DeleteRequest struct {
	AppName   string
	UserID    string
	SessionID string
	Filename  string
}

// VersionsRequest is a request to get the version count of an artifact.
type VersionsRequest struct {
	AppName   string
	UserID    string
	SessionID string
	Filename  string
}

// InMemoryService returns a thread-safe in-memory artifact service.
func InMemoryService() Service {
	return &inMemoryService{
		store: make(map[string]map[string][]*genai.Part),
	}
}

type inMemoryService struct {
	mu    sync.RWMutex
	store map[string]map[string][]*genai.Part // storageKey -> filename -> versions
}

// isUserScoped returns true if the filename is user-scoped (shared across sessions).
func isUserScoped(filename string) bool {
	return len(filename) > 5 && filename[:5] == "user:"
}

func sessionKey(appName, userID, sessionID string) string {
	return appName + "|" + userID + "|" + sessionID
}

func userKey(appName, userID string) string {
	return appName + "|" + userID + "|__user__"
}

// storageKey returns the appropriate storage key based on filename scope.
func storageKey(appName, userID, sessionID, filename string) string {
	if isUserScoped(filename) {
		return userKey(appName, userID)
	}
	return sessionKey(appName, userID, sessionID)
}

func (s *inMemoryService) Save(_ context.Context, req *SaveRequest) (int64, error) {
	if req.Filename == "" {
		return 0, fmt.Errorf("artifact: filename is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := storageKey(req.AppName, req.UserID, req.SessionID, req.Filename)
	if s.store[key] == nil {
		s.store[key] = make(map[string][]*genai.Part)
	}
	s.store[key][req.Filename] = append(s.store[key][req.Filename], req.Data)
	version := int64(len(s.store[key][req.Filename]))
	return version, nil
}

func (s *inMemoryService) Load(_ context.Context, req *LoadRequest) (*LoadResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := storageKey(req.AppName, req.UserID, req.SessionID, req.Filename)
	files, ok := s.store[key]
	if !ok {
		return nil, fmt.Errorf("artifact: session not found")
	}
	versions, ok := files[req.Filename]
	if !ok || len(versions) == 0 {
		return nil, fmt.Errorf("artifact: %q not found", req.Filename)
	}

	v := req.Version
	if v <= 0 || int(v) > len(versions) {
		v = int64(len(versions)) // latest
	}
	return &LoadResponse{
		Data:    versions[v-1],
		Version: v,
	}, nil
}

func (s *inMemoryService) List(_ context.Context, req *ListRequest) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// List session-scoped artifacts
	key := sessionKey(req.AppName, req.UserID, req.SessionID)
	nameSet := make(map[string]bool)

	if files, ok := s.store[key]; ok {
		for name := range files {
			nameSet[name] = true
		}
	}

	// Also list user-scoped artifacts
	uKey := userKey(req.AppName, req.UserID)
	if files, ok := s.store[uKey]; ok {
		for name := range files {
			nameSet[name] = true
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	return names, nil
}

func (s *inMemoryService) Delete(_ context.Context, req *DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := storageKey(req.AppName, req.UserID, req.SessionID, req.Filename)
	if files, ok := s.store[key]; ok {
		delete(files, req.Filename)
	}
	return nil
}

func (s *inMemoryService) Versions(_ context.Context, req *VersionsRequest) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := storageKey(req.AppName, req.UserID, req.SessionID, req.Filename)
	files, ok := s.store[key]
	if !ok {
		return 0, nil
	}
	versions, ok := files[req.Filename]
	if !ok {
		return 0, nil
	}
	return int64(len(versions)), nil
}

var _ Service = (*inMemoryService)(nil)
