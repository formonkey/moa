package skillopt

import (
	"fmt"
	"strings"
	"time"
)

// BatchMetrics holds the metrics for a single batch within an epoch.
type BatchMetrics struct {
	Epoch     int     `json:"epoch"`
	Batch     int     `json:"batch"`
	AvgScore  float64 `json:"avg_score"`
	MaxScore  float64 `json:"max_score"`
	MinScore  float64 `json:"min_score"`
	TasksDone int     `json:"tasks_done"`
}

// EpochMetrics holds the aggregate metrics for one full training epoch.
type EpochMetrics struct {
	Epoch         int           `json:"epoch"`
	TrainScore    float64       `json:"train_score"`
	ValScore      float64       `json:"val_score"`
	EditsApplied  int           `json:"edits_applied"`
	EditsRejected int           `json:"edits_rejected"`
	Duration      time.Duration `json:"duration"`
	SkillLen      int           `json:"skill_len"` // Length of skill content in chars.
}

// String returns a human-readable summary of the epoch metrics.
func (m EpochMetrics) String() string {
	return fmt.Sprintf("epoch=%d train=%.3f val=%.3f edits=+%d/-%d skill_len=%d dur=%s",
		m.Epoch, m.TrainScore, m.ValScore, m.EditsApplied, m.EditsRejected,
		m.SkillLen, m.Duration.Round(time.Millisecond))
}

// TrainHistory is a chronological record of all epoch metrics during training.
type TrainHistory []EpochMetrics

// BestEpoch returns the epoch with the highest validation score.
func (h TrainHistory) BestEpoch() (EpochMetrics, int) {
	if len(h) == 0 {
		return EpochMetrics{}, -1
	}
	best := h[0]
	bestIdx := 0
	for i, m := range h[1:] {
		if m.ValScore > best.ValScore {
			best = m
			bestIdx = i + 1
		}
	}
	return best, bestIdx
}

// Summary returns a formatted multi-line summary of training history.
func (h TrainHistory) Summary() string {
	if len(h) == 0 {
		return "No training history."
	}
	var b strings.Builder
	b.WriteString("=== Training History ===\n")
	for _, m := range h {
		b.WriteString(fmt.Sprintf("  %s\n", m))
	}
	best, idx := h.BestEpoch()
	b.WriteString(fmt.Sprintf("\nBest: epoch %d (val=%.3f) [index=%d]\n", best.Epoch, best.ValScore, idx))
	return b.String()
}
