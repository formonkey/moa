package skillopt

import (
	"fmt"

	"github.com/formonkey/moa/model"
)

// Config configures a skill optimization run.
type Config struct {
	// TargetModel is the frozen LLM that executes tasks using the skill.
	// This is the model whose performance we want to improve.
	TargetModel model.LLM

	// OptimizerModel is the LLM that analyzes trajectories and proposes edits.
	// Typically a more powerful model than TargetModel.
	// If nil, defaults to TargetModel.
	OptimizerModel model.LLM

	// --- Training Hyperparameters ---

	// Epochs is the number of full passes over the training dataset.
	// Default: 3.
	Epochs int

	// BatchSize is the number of tasks per batch before an update.
	// Default: 5.
	BatchSize int

	// LearningRate controls the maximum number of edits per update step.
	// Acts as a "textual learning rate" to prevent destructive rewrites.
	// Default: 3.
	LearningRate int

	// SuccessThreshold is the minimum score for a trajectory to be
	// considered successful. Used for partitioning trajectories.
	// Default: 0.8.
	SuccessThreshold float64

	// --- Skill ---

	// InitialSkill is the starting content of the skill document.
	// Can be empty to start from scratch (the optimizer will build it up).
	InitialSkill string

	// --- Scoring ---

	// Scorer evaluates agent trajectories. If nil, defaults to ContainsScorer.
	Scorer Scorer

	// --- IO ---

	// OutputDir is where best_skill.md and checkpoints are saved.
	// If empty, no files are written to disk.
	OutputDir string

	// --- Callbacks ---

	// OnEpochStart is called at the beginning of each epoch.
	OnEpochStart func(epoch int)

	// OnBatchDone is called after each batch is processed.
	OnBatchDone func(epoch, batch int, metrics BatchMetrics)

	// OnEditApplied is called when an edit is accepted (passed validation gate).
	OnEditApplied func(edit Edit)

	// OnEditRejected is called when an edit is rejected.
	OnEditRejected func(edit Edit, reason string)

	// OnEpochEnd is called at the end of each epoch with aggregate metrics.
	OnEpochEnd func(epoch int, metrics EpochMetrics)

	// --- Advanced ---

	// MaxSkillLen caps the skill document length in characters.
	// If exceeded, the optimizer is asked to consolidate.
	// Default: 10000 (0 = no limit).
	MaxSkillLen int

	// RejectedEditBuffer keeps rejected edits for reconsideration in the
	// next epoch's meta-update phase. Default: true.
	RejectedEditBuffer bool
}

// defaults fills in zero-valued fields with sensible defaults.
func (c *Config) defaults() {
	if c.OptimizerModel == nil {
		c.OptimizerModel = c.TargetModel
	}
	if c.Epochs <= 0 {
		c.Epochs = 3
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 5
	}
	if c.LearningRate <= 0 {
		c.LearningRate = 3
	}
	if c.SuccessThreshold <= 0 {
		c.SuccessThreshold = 0.8
	}
	if c.Scorer == nil {
		c.Scorer = ContainsScorer()
	}
	if c.MaxSkillLen <= 0 {
		c.MaxSkillLen = 10000
	}
}

// validate checks that the configuration is valid.
func (c *Config) validate() error {
	if c.TargetModel == nil {
		return fmt.Errorf("skillopt: TargetModel is required")
	}
	if c.Epochs <= 0 {
		return fmt.Errorf("skillopt: Epochs must be > 0")
	}
	if c.BatchSize <= 0 {
		return fmt.Errorf("skillopt: BatchSize must be > 0")
	}
	if c.LearningRate <= 0 {
		return fmt.Errorf("skillopt: LearningRate must be > 0")
	}
	return nil
}
