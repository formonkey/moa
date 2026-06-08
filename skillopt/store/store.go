package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EpochMetrics mirrors skillopt.EpochMetrics for JSON serialization.
// Duplicated here to avoid import cycles.
type EpochMetrics struct {
	Epoch         int           `json:"epoch"`
	TrainScore    float64       `json:"train_score"`
	ValScore      float64       `json:"val_score"`
	EditsApplied  int           `json:"edits_applied"`
	EditsRejected int           `json:"edits_rejected"`
	Duration      time.Duration `json:"duration"`
	SkillLen      int           `json:"skill_len"`
}

// Store persists the state of a training run.
type Store interface {
	// SaveCheckpoint saves a snapshot of the skill and metrics for a given epoch.
	SaveCheckpoint(epoch int, skill string, metrics EpochMetrics) error

	// LoadBestSkill loads the best skill saved so far.
	LoadBestSkill() (string, error)

	// SaveHistory saves the complete training history.
	SaveHistory(history []EpochMetrics) error

	// SaveBestSkill persists the best skill as best_skill.md.
	SaveBestSkill(skill string) error
}

// --- FileStore ---

// FileStore implements Store using the local filesystem.
//
// Directory structure:
//
//	dir/
//	├── best_skill.md
//	├── history.json
//	└── checkpoints/
//	    ├── epoch_0.md
//	    ├── epoch_0_metrics.json
//	    ├── epoch_1.md
//	    └── ...
type FileStore struct {
	dir string
}

// NewFileStore creates a FileStore rooted at the given directory.
// The directory and its subdirectories are created if needed.
func NewFileStore(dir string) (*FileStore, error) {
	cpDir := filepath.Join(dir, "checkpoints")
	if err := os.MkdirAll(cpDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating store directory: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

// SaveCheckpoint saves the skill content and metrics for the given epoch.
func (s *FileStore) SaveCheckpoint(epoch int, skill string, metrics EpochMetrics) error {
	// Save skill
	skillPath := filepath.Join(s.dir, "checkpoints", fmt.Sprintf("epoch_%d.md", epoch))
	if err := os.WriteFile(skillPath, []byte(skill), 0o644); err != nil {
		return fmt.Errorf("saving skill checkpoint: %w", err)
	}

	// Save metrics
	metricsData, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metrics: %w", err)
	}
	metricsPath := filepath.Join(s.dir, "checkpoints", fmt.Sprintf("epoch_%d_metrics.json", epoch))
	if err := os.WriteFile(metricsPath, metricsData, 0o644); err != nil {
		return fmt.Errorf("saving metrics checkpoint: %w", err)
	}

	return nil
}

// LoadBestSkill reads the best_skill.md file.
func (s *FileStore) LoadBestSkill() (string, error) {
	path := filepath.Join(s.dir, "best_skill.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("loading best skill: %w", err)
	}
	return string(data), nil
}

// SaveBestSkill writes the best skill to best_skill.md.
func (s *FileStore) SaveBestSkill(skill string) error {
	path := filepath.Join(s.dir, "best_skill.md")
	if err := os.WriteFile(path, []byte(skill), 0o644); err != nil {
		return fmt.Errorf("saving best skill: %w", err)
	}
	return nil
}

// SaveHistory writes the training history to history.json.
func (s *FileStore) SaveHistory(history []EpochMetrics) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling history: %w", err)
	}
	path := filepath.Join(s.dir, "history.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("saving history: %w", err)
	}
	return nil
}

// --- MemoryStore ---

// MemoryStore implements Store entirely in memory (no disk IO).
// Useful for testing.
type MemoryStore struct {
	bestSkill   string
	checkpoints map[int]string
	history     []EpochMetrics
}

// NewMemoryStore creates an in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		checkpoints: make(map[int]string),
	}
}

func (s *MemoryStore) SaveCheckpoint(epoch int, skill string, _ EpochMetrics) error {
	s.checkpoints[epoch] = skill
	return nil
}

func (s *MemoryStore) LoadBestSkill() (string, error) {
	if s.bestSkill == "" {
		return "", fmt.Errorf("no best skill saved")
	}
	return s.bestSkill, nil
}

func (s *MemoryStore) SaveBestSkill(skill string) error {
	s.bestSkill = skill
	return nil
}

func (s *MemoryStore) SaveHistory(history []EpochMetrics) error {
	s.history = history
	return nil
}
