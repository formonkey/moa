package badger

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/formonkey/moa/memory"
)

type Store struct {
	db   *badger.DB
	path string
}

func NewStore(path string) *Store {
	if path == "" {
		path = "./.brain_data"
	}
	return &Store{path: path}
}

func (s *Store) Init() error {
	opt := badger.DefaultOptions(s.path)
	opt.Logger = nil // Disable verbose logging
	db, err := badger.Open(opt)
	if err != nil {
		return fmt.Errorf("failed to open badger db: %w", err)
	}
	s.db = db
	return nil
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) AddMessage(msg memory.Message) error {
	if msg.Timestamp == 0 {
		msg.Timestamp = time.Now().UnixNano()
	}

	key := fmt.Sprintf("session:%s:msg:%020d", msg.SessionID, msg.Timestamp)
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})
}

func (s *Store) GetSessionHistory(sessionID string) ([]memory.Message, error) {
	var messages []memory.Message

	err := s.db.View(func(txn *badger.Txn) error {
		prefix := []byte(fmt.Sprintf("session:%s:msg:", sessionID))
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(v []byte) error {
				var msg memory.Message
				if err := json.Unmarshal(v, &msg); err != nil {
					return err
				}
				messages = append(messages, msg)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Ensure chronological order
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp < messages[j].Timestamp
	})

	return messages, nil
}
