package skillopt

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/skillopt/store"
)

// TrainResult is the output of a complete training run.
type TrainResult struct {
	// BestSkill is the content of the optimized skill document.
	BestSkill string
	// BestScore is the validation score achieved by BestSkill.
	BestScore float64
	// BestEpoch is the epoch that produced BestSkill.
	BestEpoch int
	// History is the per-epoch metrics over the entire training run.
	History TrainHistory
	// TotalEdits is the total number of edits applied.
	TotalEdits int
	// RejectedEdits is the total number of edits rejected by the validation gate.
	RejectedEdits int
	// Duration is the total wall-clock time for training.
	Duration time.Duration
}

// Optimizer implements the text-space optimization loop.
type Optimizer struct {
	cfg       Config
	reflector *Reflector
	store     store.Store
}

// New creates a new Optimizer with the given configuration.
func New(cfg Config) (*Optimizer, error) {
	cfg.defaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opt := &Optimizer{
		cfg:       cfg,
		reflector: NewReflector(cfg.OptimizerModel, cfg.LearningRate),
	}

	// Set up persistence
	if cfg.OutputDir != "" {
		fs, err := store.NewFileStore(cfg.OutputDir)
		if err != nil {
			return nil, fmt.Errorf("creating file store: %w", err)
		}
		opt.store = fs
	} else {
		opt.store = store.NewMemoryStore()
	}

	return opt, nil
}

// Train runs the complete optimization loop over the dataset.
func (o *Optimizer) Train(ctx context.Context, dataset Dataset) (*TrainResult, error) {
	if err := dataset.Validate(); err != nil {
		return nil, fmt.Errorf("dataset validation: %w", err)
	}

	start := time.Now()
	currentSkill := o.cfg.InitialSkill
	bestSkill := currentSkill
	bestValScore := -1.0
	bestEpoch := 0

	var history TrainHistory
	totalEditsApplied := 0
	totalEditsRejected := 0
	var rejectedBuffer []Edit

	for epoch := 0; epoch < o.cfg.Epochs; epoch++ {
		if ctx.Err() != nil {
			break
		}

		epochStart := time.Now()
		if o.cfg.OnEpochStart != nil {
			o.cfg.OnEpochStart(epoch)
		}

		epochEditsApplied := 0
		epochEditsRejected := 0

		// --- Meta-update from rejected buffer (epoch > 0) ---
		if epoch > 0 && o.cfg.RejectedEditBuffer && len(rejectedBuffer) > 0 {
			log.Printf("[skillopt] epoch %d: re-evaluating %d rejected edits from previous epoch",
				epoch, len(rejectedBuffer))

			// Try applying buffered edits with current context
			for _, edit := range rejectedBuffer {
				candidate, applied, _ := ApplyEdits(currentSkill, []Edit{edit})
				if len(applied) > 0 {
					// Validate the candidate
					candidateScore := o.evaluateSkill(ctx, candidate, dataset.Val)
					currentScore := o.evaluateSkill(ctx, currentSkill, dataset.Val)
					if candidateScore > currentScore {
						currentSkill = candidate
						epochEditsApplied++
						if o.cfg.OnEditApplied != nil {
							o.cfg.OnEditApplied(edit)
						}
					}
				}
			}
			rejectedBuffer = nil
		}

		// --- Batch loop (forward pass + backward pass) ---
		batches := dataset.Batches(o.cfg.BatchSize)
		for batchIdx, batch := range batches {
			if ctx.Err() != nil {
				break
			}

			// Forward pass: execute tasks with current skill
			trajectories := o.executeBatch(ctx, currentSkill, batch)

			// Score trajectories
			avgScore := ScoreTrajectories(trajectories, o.cfg.Scorer)

			batchMetrics := BatchMetrics{
				Epoch:     epoch,
				Batch:     batchIdx,
				AvgScore:  avgScore,
				TasksDone: len(trajectories),
			}
			if len(trajectories) > 0 {
				batchMetrics.MinScore = trajectories[0].Score
				batchMetrics.MaxScore = trajectories[0].Score
				for _, t := range trajectories[1:] {
					if t.Score < batchMetrics.MinScore {
						batchMetrics.MinScore = t.Score
					}
					if t.Score > batchMetrics.MaxScore {
						batchMetrics.MaxScore = t.Score
					}
				}
			}

			if o.cfg.OnBatchDone != nil {
				o.cfg.OnBatchDone(epoch, batchIdx, batchMetrics)
			}

			// Backward pass: reflect on trajectories to propose edits
			edits, err := o.reflector.Reflect(ctx, currentSkill, trajectories)
			if err != nil {
				log.Printf("[skillopt] epoch %d batch %d: reflector error: %v", epoch, batchIdx, err)
				continue
			}

			if len(edits) == 0 {
				continue
			}

			// Apply edits to produce candidate skill
			candidateSkill, applied, skipped := ApplyEdits(currentSkill, edits)

			// Log skipped edits
			for _, s := range skipped {
				log.Printf("[skillopt] epoch %d batch %d: skipped edit (target not found): %s", epoch, batchIdx, s)
			}

			if len(applied) == 0 {
				continue
			}

			// Enforce max skill length
			if o.cfg.MaxSkillLen > 0 && len(candidateSkill) > o.cfg.MaxSkillLen {
				log.Printf("[skillopt] epoch %d batch %d: candidate skill too long (%d > %d), truncating edits",
					epoch, batchIdx, len(candidateSkill), o.cfg.MaxSkillLen)
				// Fall back to current skill (don't accept the edit)
				for _, a := range applied {
					epochEditsRejected++
					rejectedBuffer = append(rejectedBuffer, a)
					if o.cfg.OnEditRejected != nil {
						o.cfg.OnEditRejected(a, "skill too long")
					}
				}
				continue
			}

			// --- Validation Gate ---
			currentValScore := o.evaluateSkill(ctx, currentSkill, dataset.Val)
			candidateValScore := o.evaluateSkill(ctx, candidateSkill, dataset.Val)

			if candidateValScore >= currentValScore {
				// Accept: candidate is at least as good
				currentSkill = candidateSkill
				epochEditsApplied += len(applied)

				for _, a := range applied {
					if o.cfg.OnEditApplied != nil {
						o.cfg.OnEditApplied(a)
					}
				}

				log.Printf("[skillopt] epoch %d batch %d: accepted %d edits (val: %.3f → %.3f)",
					epoch, batchIdx, len(applied), currentValScore, candidateValScore)
			} else {
				// Reject: candidate is worse
				epochEditsRejected += len(applied)

				for _, a := range applied {
					rejectedBuffer = append(rejectedBuffer, a)
					if o.cfg.OnEditRejected != nil {
						o.cfg.OnEditRejected(a, fmt.Sprintf("val score decreased: %.3f → %.3f",
							currentValScore, candidateValScore))
					}
				}

				log.Printf("[skillopt] epoch %d batch %d: rejected %d edits (val: %.3f → %.3f)",
					epoch, batchIdx, len(applied), currentValScore, candidateValScore)
			}
		}

		// --- End of epoch ---
		valScore := o.evaluateSkill(ctx, currentSkill, dataset.Val)
		trainScore := o.evaluateSkill(ctx, currentSkill, dataset.Train)

		epochMetrics := EpochMetrics{
			Epoch:         epoch,
			TrainScore:    trainScore,
			ValScore:      valScore,
			EditsApplied:  epochEditsApplied,
			EditsRejected: epochEditsRejected,
			Duration:      time.Since(epochStart),
			SkillLen:      len(currentSkill),
		}
		history = append(history, epochMetrics)
		totalEditsApplied += epochEditsApplied
		totalEditsRejected += epochEditsRejected

		// Checkpoint
		if o.store != nil {
			storeMetrics := store.EpochMetrics{
				Epoch:         epochMetrics.Epoch,
				TrainScore:    epochMetrics.TrainScore,
				ValScore:      epochMetrics.ValScore,
				EditsApplied:  epochMetrics.EditsApplied,
				EditsRejected: epochMetrics.EditsRejected,
				Duration:      epochMetrics.Duration,
				SkillLen:      epochMetrics.SkillLen,
			}
			if err := o.store.SaveCheckpoint(epoch, currentSkill, storeMetrics); err != nil {
				log.Printf("[skillopt] checkpoint save error: %v", err)
			}
		}

		// Track best
		if valScore > bestValScore {
			bestValScore = valScore
			bestSkill = currentSkill
			bestEpoch = epoch
			if o.store != nil {
				if err := o.store.SaveBestSkill(currentSkill); err != nil {
					log.Printf("[skillopt] best skill save error: %v", err)
				}
			}
		}

		if o.cfg.OnEpochEnd != nil {
			o.cfg.OnEpochEnd(epoch, epochMetrics)
		}

		log.Printf("[skillopt] %s", epochMetrics)
	}

	// Save final history
	if o.store != nil && len(history) > 0 {
		storeHistory := make([]store.EpochMetrics, len(history))
		for i, h := range history {
			storeHistory[i] = store.EpochMetrics{
				Epoch:         h.Epoch,
				TrainScore:    h.TrainScore,
				ValScore:      h.ValScore,
				EditsApplied:  h.EditsApplied,
				EditsRejected: h.EditsRejected,
				Duration:      h.Duration,
				SkillLen:      h.SkillLen,
			}
		}
		if err := o.store.SaveHistory(storeHistory); err != nil {
			log.Printf("[skillopt] history save error: %v", err)
		}
	}

	return &TrainResult{
		BestSkill:     bestSkill,
		BestScore:     bestValScore,
		BestEpoch:     bestEpoch,
		History:       history,
		TotalEdits:    totalEditsApplied,
		RejectedEdits: totalEditsRejected,
		Duration:      time.Since(start),
	}, nil
}

// executeBatch runs the target agent on a batch of tasks using the current skill
// and returns the scored trajectories.
func (o *Optimizer) executeBatch(ctx context.Context, skill string, tasks []Task) []Trajectory {
	trajectories := make([]Trajectory, 0, len(tasks))

	for _, task := range tasks {
		if ctx.Err() != nil {
			break
		}

		traj := o.executeTask(ctx, skill, task)
		trajectories = append(trajectories, traj)
	}

	return trajectories
}

// executeTask runs the target model on a single task with the skill as system instruction.
func (o *Optimizer) executeTask(ctx context.Context, skill string, task Task) Trajectory {
	start := time.Now()

	// Build the prompt with optional context
	userPrompt := task.Input
	if task.Context != "" {
		userPrompt = fmt.Sprintf("Context: %s\n\nQuestion: %s", task.Context, task.Input)
	}

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(userPrompt, "user"),
		},
		Config: &genai.GenerateContentConfig{
			Temperature: genai.Ptr(float32(0.2)),
		},
	}

	// Set system instruction (skill document)
	if skill != "" {
		req.SystemInstruction = genai.NewContentFromText(skill, "user")
	}

	var responseText strings.Builder
	var steps []Step

	// Add user step
	steps = append(steps, Step{
		Role:    "user",
		Content: userPrompt,
	})

	// Execute
	for resp, err := range o.cfg.TargetModel.GenerateContent(ctx, req, false) {
		if err != nil {
			steps = append(steps, Step{
				Role:    "error",
				Content: err.Error(),
			})
			break
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText.WriteString(part.Text)
					steps = append(steps, Step{
						Role:    "assistant",
						Content: part.Text,
					})
				}
				if part.FunctionCall != nil {
					steps = append(steps, Step{
						Role: "assistant",
						Tool: &ToolCallRecord{
							Name: part.FunctionCall.Name,
							Args: fmt.Sprintf("%v", part.FunctionCall.Args),
						},
					})
				}
			}
		}
	}

	return Trajectory{
		TaskID:   task.ID,
		Input:    task.Input,
		Steps:    steps,
		Output:   responseText.String(),
		Expected: task.Expected,
		Duration: time.Since(start),
	}
}

// evaluateSkill runs the target model on a set of tasks with the given skill
// and returns the average score.
func (o *Optimizer) evaluateSkill(ctx context.Context, skill string, tasks []Task) float64 {
	if len(tasks) == 0 {
		return 0.0
	}

	trajectories := o.executeBatch(ctx, skill, tasks)
	return ScoreTrajectories(trajectories, o.cfg.Scorer)
}
