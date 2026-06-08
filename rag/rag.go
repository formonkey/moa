// Package rag provides document indexing and search backed by BadgerDB.
//
// It splits markdown files into sections (by ## and ### headers) and stores
// them in Badger for fast keyword search. A checksum mechanism prevents
// re-indexing unchanged files.
package rag

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

// Section represents a single searchable chunk of documentation.
type Section struct {
	File    string `json:"file"`    // source filename
	Title   string `json:"title"`   // section header (## or ###)
	Content string `json:"content"` // full section text including header
}

// Store is a Badger-backed document index.
type Store struct {
	db   *badger.DB
	path string
}

// NewStore creates a new RAG store at the given path.
// If path is empty, defaults to "./.moa_rag".
func NewStore(path string) (*Store, error) {
	if path == "" {
		path = "./.moa_rag"
	}

	opt := badger.DefaultOptions(path)
	opt.Logger = nil // suppress verbose badger logs

	db, err := badger.Open(opt)
	if err != nil {
		return nil, fmt.Errorf("rag: failed to open badger at %s: %w", path, err)
	}

	return &Store{db: db, path: path}, nil
}

// Close shuts down the Badger database.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// IndexFiles indexes multiple markdown files. Skips files whose checksum
// hasn't changed since last indexing.
func (s *Store) IndexFiles(paths []string) error {
	for _, p := range paths {
		if err := s.IndexFile(p); err != nil {
			return err
		}
	}
	return nil
}

// IndexFile reads a markdown file, splits it into sections by ## and ###
// headers, and stores each section in Badger. Uses SHA-256 checksum to
// skip re-indexing if the file hasn't changed.
func (s *Store) IndexFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("rag: cannot read %s: %w", path, err)
	}

	// Compute checksum
	checksum := fmt.Sprintf("%x", sha256.Sum256(data))

	// Check if file is already indexed with same checksum
	checksumKey := fmt.Sprintf("rag:checksum:%s", path)
	stored, err := s.get(checksumKey)
	if err == nil && stored == checksum {
		return nil // file unchanged, skip
	}

	// Clear old sections for this file before re-indexing
	if err := s.deletePrefix(fmt.Sprintf("rag:section:%s:", path)); err != nil {
		return fmt.Errorf("rag: failed to clear old sections for %s: %w", path, err)
	}

	// Split into sections
	sections := splitMarkdown(path, string(data))

	// Store sections
	err = s.db.Update(func(txn *badger.Txn) error {
		for i, sec := range sections {
			key := fmt.Sprintf("rag:section:%s:%04d", path, i)
			val, err := json.Marshal(sec)
			if err != nil {
				return err
			}
			if err := txn.Set([]byte(key), val); err != nil {
				return err
			}
		}
		// Store checksum
		return txn.Set([]byte(checksumKey), []byte(checksum))
	})
	if err != nil {
		return fmt.Errorf("rag: failed to index %s: %w", path, err)
	}

	return nil
}

// Search finds sections whose content contains the query (case-insensitive).
// Returns at most maxResults sections.
func (s *Store) Search(query string, maxResults int) ([]Section, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	q := strings.ToLower(query)

	var results []Section

	err := s.db.View(func(txn *badger.Txn) error {
		prefix := []byte("rag:section:")
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if len(results) >= maxResults {
				break
			}
			item := it.Item()
			err := item.Value(func(v []byte) error {
				var sec Section
				if err := json.Unmarshal(v, &sec); err != nil {
					return err
				}
				if strings.Contains(strings.ToLower(sec.Content), q) {
					results = append(results, sec)
				}
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

// --- helpers ---

func (s *Store) get(key string) (string, error) {
	var val string
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(v []byte) error {
			val = string(v)
			return nil
		})
	})
	return val, err
}

func (s *Store) deletePrefix(prefix string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		pfx := []byte(prefix)
		var keys [][]byte
		for it.Seek(pfx); it.ValidForPrefix(pfx); it.Next() {
			key := it.Item().KeyCopy(nil)
			keys = append(keys, key)
		}
		for _, k := range keys {
			if err := txn.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
}

// splitMarkdown splits a markdown document into sections by ## and ### headers.
func splitMarkdown(filename, content string) []Section {
	lines := strings.Split(content, "\n")
	var sections []Section
	var current strings.Builder
	var currentTitle string

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			// Flush previous section
			if current.Len() > 0 {
				sections = append(sections, Section{
					File:    filename,
					Title:   currentTitle,
					Content: current.String(),
				})
			}
			current.Reset()
			currentTitle = line
			current.WriteString(line + "\n")
		} else {
			current.WriteString(line + "\n")
		}
	}
	// Don't forget last section
	if current.Len() > 0 {
		sections = append(sections, Section{
			File:    filename,
			Title:   currentTitle,
			Content: current.String(),
		})
	}

	return sections
}
