// Package learnings provides a persistent learning store for MOA agents.
//
// Agents save lessons learned from user feedback (corrections, preferences,
// patterns) into BadgerDB. Before starting work, agents query past learnings
// to avoid repeating mistakes.
//
// Usage:
//
//	store, _ := learnings.NewStore("./.moa_learnings")
//	learnings.Register(store)
//	defer store.Close()
//	// Tools available: save_learning, search_learnings
package learnings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"

	"github.com/formonkey/moa/configurable"
	"github.com/formonkey/moa/tool"
	"github.com/formonkey/moa/tool/functiontool"
)

// Learning represents a single lesson learned from user feedback.
type Learning struct {
	ID        string `json:"id"`
	Category  string `json:"category"`  // e.g., "angular", "naming", "architecture", "style"
	Lesson    string `json:"lesson"`    // what the agent learned
	Context   string `json:"context"`   // the original task/situation
	Source    string `json:"source"`    // "user_feedback", "build_error", "review"
	CreatedAt int64  `json:"created_at"`
}

// Store persists learnings in BadgerDB.
type Store struct {
	db *badgerdb.DB
}

// NewStore opens or creates a learning store at the given path.
func NewStore(path string) (*Store, error) {
	if path == "" {
		path = "./.moa_learnings"
	}
	opts := badgerdb.DefaultOptions(path).
		WithLogger(nil).
		WithLoggingLevel(badgerdb.ERROR)
	db, err := badgerdb.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("learnings: failed to open store at %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close shuts down the store.
func (s *Store) Close() error {
	return s.db.Close()
}

// Save stores a new learning.
func (s *Store) Save(category, lesson, taskContext, source string) (*Learning, error) {
	l := &Learning{
		ID:        fmt.Sprintf("learn-%d", time.Now().UnixNano()),
		Category:  strings.ToLower(strings.TrimSpace(category)),
		Lesson:    lesson,
		Context:   taskContext,
		Source:     source,
		CreatedAt: time.Now().Unix(),
	}

	data, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}

	err = s.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set([]byte("learning:"+l.ID), data)
	})
	if err != nil {
		return nil, fmt.Errorf("learnings: save failed: %w", err)
	}
	return l, nil
}

// Search finds learnings matching the query (case-insensitive substring match
// on category, lesson, and context fields).
func (s *Store) Search(query string, maxResults int) ([]Learning, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	query = strings.ToLower(query)

	var results []Learning
	err := s.db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("learning:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if len(results) >= maxResults {
				break
			}
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var l Learning
				if err := json.Unmarshal(val, &l); err != nil {
					return nil // skip corrupt entries
				}

				// Match against category, lesson, and context
				lower := strings.ToLower(l.Category + " " + l.Lesson + " " + l.Context)
				if strings.Contains(lower, query) {
					results = append(results, l)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("learnings: search failed: %w", err)
	}
	return results, nil
}

// All returns all learnings (limited).
func (s *Store) All(maxResults int) ([]Learning, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	var results []Learning
	err := s.db.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("learning:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if len(results) >= maxResults {
				break
			}
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var l Learning
				if err := json.Unmarshal(val, &l); err != nil {
					return nil
				}
				results = append(results, l)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return results, err
}

// --- Tools ---

// Register creates learning tools and registers them.
func Register(store *Store) error {
	tools, err := NewToolset(store)
	if err != nil {
		return fmt.Errorf("learnings: %w", err)
	}
	for _, t := range tools {
		toolRef := t
		_ = configurable.RegisterToolFactory(t.Name(), func(ctx context.Context, args map[string]any) (tool.Tool, error) {
			return toolRef, nil
		})
	}
	return nil
}

// NewToolset creates all learning tools.
func NewToolset(store *Store) ([]tool.Tool, error) {
	save, err := newSaveLearning(store)
	if err != nil {
		return nil, err
	}
	search, err := newSearchLearnings(store)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{save, search}, nil
}

// --- save_learning ---

type saveLearningArgs struct {
	Category string `json:"category" jsonschema:"description=Category of the learning (e.g. naming/architecture/style/patterns/errors),required"`
	Lesson   string `json:"lesson" jsonschema:"description=What you learned. Be specific and actionable (e.g. 'Use private readonly for inject() not just private'),required"`
	Context  string `json:"context" jsonschema:"description=The task or situation where this was learned"`
}
type saveLearningResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func newSaveLearning(store *Store) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "save_learning",
		Description: "Save a lesson learned from user feedback or corrections. Use this to remember mistakes and avoid repeating them in future tasks.",
	}, func(ctx context.Context, args saveLearningArgs) (saveLearningResult, error) {
		l, err := store.Save(args.Category, args.Lesson, args.Context, "user_feedback")
		if err != nil {
			return saveLearningResult{}, err
		}
		return saveLearningResult{ID: l.ID, Status: "saved"}, nil
	})
}

// --- search_learnings ---

type searchLearningsArgs struct {
	Query      string `json:"query" jsonschema:"description=Search term to find relevant past learnings,required"`
	MaxResults int    `json:"max_results" jsonschema:"description=Maximum results (default 10)"`
}
type searchLearningsResult struct {
	Learnings []string `json:"learnings"`
	Count     int      `json:"count"`
}

func newSearchLearnings(store *Store) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "search_learnings",
		Description: "Search past learnings and corrections. Use this BEFORE starting any task to check if there are relevant lessons from previous work.",
	}, func(ctx context.Context, args searchLearningsArgs) (searchLearningsResult, error) {
		results, err := store.Search(args.Query, args.MaxResults)
		if err != nil {
			return searchLearningsResult{}, err
		}

		var lines []string
		for _, l := range results {
			line := fmt.Sprintf("[%s] %s", l.Category, l.Lesson)
			if l.Context != "" {
				line += fmt.Sprintf(" (context: %s)", l.Context)
			}
			lines = append(lines, line)
		}

		return searchLearningsResult{
			Learnings: lines,
			Count:     len(lines),
		}, nil
	})
}
