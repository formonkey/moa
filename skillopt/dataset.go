package skillopt

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// Task is a single item in the training dataset.
type Task struct {
	// ID uniquely identifies this task.
	ID string `json:"id"`
	// Input is the user message or question to send to the agent.
	Input string `json:"input"`
	// Expected contains acceptable answers for scoring.
	Expected []string `json:"expected"`
	// Context provides additional information the agent may need.
	Context string `json:"context,omitempty"`
	// Metadata holds arbitrary key-value pairs for custom scoring.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Dataset holds the training, validation, and test splits.
type Dataset struct {
	Train []Task `json:"train"`
	Val   []Task `json:"val"`
	Test  []Task `json:"test,omitempty"`
}

// LoadFromDir loads a dataset from a directory structure:
//
//	dir/
//	├── train/items.json  (or train.json)
//	├── val/items.json    (or val.json)
//	└── test/items.json   (or test.json, optional)
func LoadFromDir(dir string) (*Dataset, error) {
	ds := &Dataset{}

	train, err := loadSplit(dir, "train")
	if err != nil {
		return nil, fmt.Errorf("loading train split: %w", err)
	}
	ds.Train = train

	val, err := loadSplit(dir, "val")
	if err != nil {
		return nil, fmt.Errorf("loading val split: %w", err)
	}
	ds.Val = val

	// Test is optional
	test, err := loadSplit(dir, "test")
	if err == nil {
		ds.Test = test
	}

	if err := ds.Validate(); err != nil {
		return nil, err
	}

	return ds, nil
}

// loadSplit tries to load tasks from dir/split/items.json or dir/split.json.
func loadSplit(dir, split string) ([]Task, error) {
	// Try dir/split/items.json first
	path := filepath.Join(dir, split, "items.json")
	if data, err := os.ReadFile(path); err == nil {
		return parseTasks(data, path)
	}

	// Try dir/split.json
	path = filepath.Join(dir, split+".json")
	if data, err := os.ReadFile(path); err == nil {
		return parseTasks(data, path)
	}

	return nil, fmt.Errorf("no data found for split %q in %s", split, dir)
}

func parseTasks(data []byte, path string) ([]Task, error) {
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return tasks, nil
}

// FromSlice creates a Dataset by randomly splitting tasks into train and val sets.
// valRatio controls the fraction used for validation (e.g., 0.2 for 20%).
func FromSlice(tasks []Task, valRatio float64) *Dataset {
	if valRatio <= 0 || valRatio >= 1 {
		valRatio = 0.2
	}

	// Shuffle a copy
	shuffled := make([]Task, len(tasks))
	copy(shuffled, tasks)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	valSize := max(1, int(float64(len(shuffled))*valRatio))
	if valSize >= len(shuffled) {
		valSize = len(shuffled) / 2
	}

	return &Dataset{
		Val:   shuffled[:valSize],
		Train: shuffled[valSize:],
	}
}

// Validate checks that the dataset has minimum requirements.
func (d *Dataset) Validate() error {
	if len(d.Train) == 0 {
		return fmt.Errorf("dataset: train split is empty")
	}
	if len(d.Val) == 0 {
		return fmt.Errorf("dataset: val split is empty")
	}
	return nil
}

// Batches splits the training set into batches of the given size.
func (d *Dataset) Batches(batchSize int) [][]Task {
	if batchSize <= 0 {
		batchSize = len(d.Train)
	}
	var batches [][]Task
	for i := 0; i < len(d.Train); i += batchSize {
		end := min(i+batchSize, len(d.Train))
		batches = append(batches, d.Train[i:end])
	}
	return batches
}
