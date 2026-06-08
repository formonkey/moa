package session

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type stateMap = map[string]any

// InMemoryService returns an in-memory implementation of the session service. Thread-safe.
func InMemoryService() Service {
	return &inMemoryService{
		sessions:  make(map[string]*inMemorySession),
		appState:  make(map[string]stateMap),
		userState: make(map[string]map[string]stateMap),
	}
}

// inMemoryService is an in-memory, thread-safe implementation of Service.
type inMemoryService struct {
	mu        sync.RWMutex
	sessions  map[string]*inMemorySession // compositeKey -> session
	appState  map[string]stateMap          // appName -> state
	userState map[string]map[string]stateMap // appName -> userID -> state
}

func compositeKey(appName, userID, sessionID string) string {
	return appName + "|" + userID + "|" + sessionID
}

func (s *inMemoryService) Create(_ context.Context, req *CreateRequest) (*CreateResponse, error) {
	if req.AppName == "" || req.UserID == "" {
		return nil, fmt.Errorf("app_name and user_id are required, got app_name: %q, user_id: %q", req.AppName, req.UserID)
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	key := compositeKey(req.AppName, req.UserID, sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[key]; ok {
		return nil, fmt.Errorf("session %s already exists", sessionID)
	}

	state := req.State
	if state == nil {
		state = make(stateMap)
	}

	sess := &inMemorySession{
		id:        sessionID,
		appName:   req.AppName,
		userID:    req.UserID,
		state:     state,
		updatedAt: time.Now(),
	}

	s.sessions[key] = sess

	// Process state deltas for app/user scoped keys
	appDelta, userDelta, _ := extractStateDeltas(req.State)
	appState := s.updateAppState(appDelta, req.AppName)
	userState := s.updateUserState(userDelta, req.AppName, req.UserID)
	sess.state = mergeStates(appState, userState, state)

	return &CreateResponse{
		Session: sess.copy(),
	}, nil
}

func (s *inMemoryService) Get(_ context.Context, req *GetRequest) (*GetResponse, error) {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return nil, fmt.Errorf("app_name, user_id, session_id are required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	key := compositeKey(req.AppName, req.UserID, req.SessionID)
	sess, ok := s.sessions[key]
	if !ok {
		return nil, fmt.Errorf("session %q not found", req.SessionID)
	}

	copied := sess.copy()
	copied.state = s.buildMergedState(sess.state, req.AppName, req.UserID)

	// Apply event filters
	filteredEvents := sess.events
	if req.NumRecentEvents > 0 {
		start := max(len(filteredEvents)-req.NumRecentEvents, 0)
		filteredEvents = filteredEvents[start:]
	}
	if !req.After.IsZero() && len(filteredEvents) > 0 {
		firstIdx := sort.Search(len(filteredEvents), func(i int) bool {
			return !filteredEvents[i].Timestamp.Before(req.After)
		})
		filteredEvents = filteredEvents[firstIdx:]
	}
	copied.events = make([]*Event, len(filteredEvents))
	copy(copied.events, filteredEvents)

	return &GetResponse{Session: copied}, nil
}

func (s *inMemoryService) List(_ context.Context, req *ListRequest) (*ListResponse, error) {
	if req.AppName == "" {
		return nil, fmt.Errorf("app_name is required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Session
	for _, sess := range s.sessions {
		if sess.appName != req.AppName {
			continue
		}
		if req.UserID != "" && sess.userID != req.UserID {
			continue
		}
		copied := sess.copy()
		copied.state = s.buildMergedState(sess.state, req.AppName, sess.userID)
		result = append(result, copied)
	}

	return &ListResponse{Sessions: result}, nil
}

func (s *inMemoryService) Delete(_ context.Context, req *DeleteRequest) error {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return fmt.Errorf("app_name, user_id, session_id are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := compositeKey(req.AppName, req.UserID, req.SessionID)
	delete(s.sessions, key)
	return nil
}

func (s *inMemoryService) AppendEvent(_ context.Context, curSession Session, event *Event) error {
	if curSession == nil {
		return fmt.Errorf("session is nil")
	}
	if event == nil {
		return fmt.Errorf("event is nil")
	}
	if event.Partial {
		return nil // don't persist partial events
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := compositeKey(curSession.AppName(), curSession.UserID(), curSession.ID())
	stored, ok := s.sessions[key]
	if !ok {
		return fmt.Errorf("session not found, cannot apply event")
	}

	// Update in-memory session state from deltas
	if event.Actions.StateDelta != nil && stored.state != nil {
		maps.Copy(stored.state, event.Actions.StateDelta)
	}

	// Deep copy the event for storage
	eventCopy := &Event{
		ID:           event.ID,
		InvocationID: event.InvocationID,
		Timestamp:    event.Timestamp,
		Author:       event.Author,
		Branch:       event.Branch,
		Actions: EventActions{
			StateDelta:                 maps.Clone(event.Actions.StateDelta),
			ArtifactDelta:              maps.Clone(event.Actions.ArtifactDelta),
			RequestedToolConfirmations: maps.Clone(event.Actions.RequestedToolConfirmations),
			TransferToAgent:            event.Actions.TransferToAgent,
			Escalate:                   event.Actions.Escalate,
			SkipSummarization:          event.Actions.SkipSummarization,
		},
		LongRunningToolIDs: slices.Clone(event.LongRunningToolIDs),
		LLMResponse:        event.LLMResponse,
	}

	// Trim temp state delta keys before persisting
	if len(eventCopy.Actions.StateDelta) > 0 {
		filtered := make(map[string]any)
		for k, v := range eventCopy.Actions.StateDelta {
			if !strings.HasPrefix(k, KeyPrefixTemp) {
				filtered[k] = v
			}
		}
		eventCopy.Actions.StateDelta = filtered
	}

	stored.events = append(stored.events, eventCopy)
	stored.updatedAt = event.Timestamp

	// Update scoped states
	if len(event.Actions.StateDelta) > 0 {
		appDelta, userDelta, _ := extractStateDeltas(event.Actions.StateDelta)
		s.updateAppState(appDelta, curSession.AppName())
		s.updateUserState(userDelta, curSession.AppName(), curSession.UserID())
	}

	// Also update the caller's session if it's an inMemorySession.
	// We must acquire the caller's own mutex to prevent data races with
	// concurrent readers (e.g., Events(), State().Get()).
	if ims, ok := curSession.(*inMemorySession); ok {
		ims.mu.Lock()
		ims.events = append(ims.events, eventCopy)
		ims.updatedAt = event.Timestamp
		if event.Actions.StateDelta != nil {
			maps.Copy(ims.state, event.Actions.StateDelta)
		}
		ims.mu.Unlock()
	}

	return nil
}

// --- internal state helpers ---

func (s *inMemoryService) updateAppState(appDelta stateMap, appName string) stateMap {
	inner, ok := s.appState[appName]
	if !ok {
		inner = make(stateMap)
		s.appState[appName] = inner
	}
	maps.Copy(inner, appDelta)
	return inner
}

func (s *inMemoryService) updateUserState(userDelta stateMap, appName, userID string) stateMap {
	usersMap, ok := s.userState[appName]
	if !ok {
		usersMap = make(map[string]stateMap)
		s.userState[appName] = usersMap
	}
	inner, ok := usersMap[userID]
	if !ok {
		inner = make(stateMap)
		usersMap[userID] = inner
	}
	maps.Copy(inner, userDelta)
	return inner
}

func (s *inMemoryService) buildMergedState(sessionState stateMap, appName, userID string) stateMap {
	appState := s.appState[appName]
	var uState stateMap
	if usersMap, ok := s.userState[appName]; ok {
		uState = usersMap[userID]
	}
	return mergeStates(appState, uState, sessionState)
}

// extractStateDeltas splits a state map into app-scoped, user-scoped, and session-scoped deltas.
func extractStateDeltas(state stateMap) (appDelta, userDelta, sessionDelta stateMap) {
	appDelta = make(stateMap)
	userDelta = make(stateMap)
	sessionDelta = make(stateMap)
	for k, v := range state {
		switch {
		case strings.HasPrefix(k, KeyPrefixApp):
			appDelta[k] = v
		case strings.HasPrefix(k, KeyPrefixUser):
			userDelta[k] = v
		default:
			sessionDelta[k] = v
		}
	}
	return
}

// mergeStates merges app, user, and session states. Session state has highest priority.
func mergeStates(appState, userState, sessionState stateMap) stateMap {
	merged := make(stateMap)
	maps.Copy(merged, appState)
	maps.Copy(merged, userState)
	maps.Copy(merged, sessionState)
	return merged
}

// --- inMemorySession implementation ---

type inMemorySession struct {
	id        string
	appName   string
	userID    string

	mu        sync.RWMutex
	events    []*Event
	state     stateMap
	updatedAt time.Time
}

func (s *inMemorySession) ID() string      { return s.id }
func (s *inMemorySession) AppName() string  { return s.appName }
func (s *inMemorySession) UserID() string   { return s.userID }

func (s *inMemorySession) State() State {
	return &inMemoryState{mu: &s.mu, state: s.state}
}

func (s *inMemorySession) Events() Events {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return eventList(slices.Clone(s.events))
}

func (s *inMemorySession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

func (s *inMemorySession) copy() *inMemorySession {
	return &inMemorySession{
		id:        s.id,
		appName:   s.appName,
		userID:    s.userID,
		state:     maps.Clone(s.state),
		events:    slices.Clone(s.events),
		updatedAt: s.updatedAt,
	}
}

// --- inMemoryState ---

type inMemoryState struct {
	mu    *sync.RWMutex
	state stateMap
}

func (s *inMemoryState) Get(key string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.state[key]
	if !ok {
		return nil, ErrStateKeyNotExist
	}
	return val, nil
}

func (s *inMemoryState) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[key] = value
	return nil
}

func (s *inMemoryState) All() iter.Seq2[string, any] {
	s.mu.RLock()
	stateCopy := maps.Clone(s.state)
	s.mu.RUnlock()
	return func(yield func(string, any) bool) {
		for k, v := range stateCopy {
			if !yield(k, v) {
				return
			}
		}
	}
}

// --- eventList ---

type eventList []*Event

func (e eventList) All() iter.Seq[*Event] {
	return func(yield func(*Event) bool) {
		for _, event := range e {
			if !yield(event) {
				return
			}
		}
	}
}

func (e eventList) Len() int { return len(e) }

func (e eventList) At(i int) *Event {
	if i >= 0 && i < len(e) {
		return e[i]
	}
	return nil
}

var _ Service = (*inMemoryService)(nil)
var _ Session = (*inMemorySession)(nil)
