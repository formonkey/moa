package skillopt

import (
	"fmt"
	"strings"
	"time"
)

// Trajectory is the complete record of an agent execution for a single task.
type Trajectory struct {
	TaskID   string        `json:"task_id"`
	Input    string        `json:"input"`
	Steps    []Step        `json:"steps"`
	Output   string        `json:"output"`
	Score    float64       `json:"score"`
	Expected []string      `json:"expected"`
	Duration time.Duration `json:"duration"`
}

// Step is a single step in a trajectory (message exchange or tool call).
type Step struct {
	Role    string          `json:"role"` // "user", "assistant", "tool"
	Content string          `json:"content"`
	Tool    *ToolCallRecord `json:"tool,omitempty"`
}

// ToolCallRecord captures a tool invocation within a trajectory step.
type ToolCallRecord struct {
	Name   string `json:"name"`
	Args   string `json:"args"`
	Result string `json:"result"`
}

// Summary returns a compact text summary of the trajectory for LLM consumption.
func (t Trajectory) Summary() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Task: %s\n", t.TaskID))
	b.WriteString(fmt.Sprintf("Input: %s\n", t.Input))
	b.WriteString(fmt.Sprintf("Output: %s\n", truncate(t.Output, 500)))
	b.WriteString(fmt.Sprintf("Score: %.3f\n", t.Score))
	if len(t.Expected) > 0 {
		b.WriteString(fmt.Sprintf("Expected: %s\n", strings.Join(t.Expected, " | ")))
	}
	b.WriteString(fmt.Sprintf("Steps: %d\n", len(t.Steps)))

	// Include tool calls for context
	for i, step := range t.Steps {
		if step.Tool != nil {
			b.WriteString(fmt.Sprintf("  step[%d] tool=%s args=%s → %s\n",
				i, step.Tool.Name, truncate(step.Tool.Args, 100), truncate(step.Tool.Result, 100)))
		}
	}
	return b.String()
}

// IsSuccess returns true if the trajectory score meets the threshold.
func (t Trajectory) IsSuccess(threshold float64) bool {
	return t.Score >= threshold
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// --- Scorers ---

// Scorer evaluates a trajectory and returns a score between 0.0 and 1.0.
type Scorer func(trajectory Trajectory) float64

// ExactMatchScorer returns 1.0 if the output exactly matches any expected answer
// (case-insensitive, trimmed), 0.0 otherwise.
func ExactMatchScorer() Scorer {
	return func(t Trajectory) float64 {
		output := strings.TrimSpace(strings.ToLower(t.Output))
		for _, exp := range t.Expected {
			if output == strings.TrimSpace(strings.ToLower(exp)) {
				return 1.0
			}
		}
		return 0.0
	}
}

// ContainsScorer returns 1.0 if the output contains any expected answer
// (case-insensitive), 0.0 otherwise.
func ContainsScorer() Scorer {
	return func(t Trajectory) float64 {
		output := strings.ToLower(t.Output)
		for _, exp := range t.Expected {
			if strings.Contains(output, strings.ToLower(exp)) {
				return 1.0
			}
		}
		return 0.0
	}
}

// FuzzyContainsScorer returns a score between 0 and 1 based on how many
// expected answers are contained in the output.
func FuzzyContainsScorer() Scorer {
	return func(t Trajectory) float64 {
		if len(t.Expected) == 0 {
			return 0.0
		}
		output := strings.ToLower(t.Output)
		matches := 0
		for _, exp := range t.Expected {
			if strings.Contains(output, strings.ToLower(exp)) {
				matches++
			}
		}
		return float64(matches) / float64(len(t.Expected))
	}
}

// CompositeScorer combines multiple scorers by averaging their results.
func CompositeScorer(scorers ...Scorer) Scorer {
	return func(t Trajectory) float64 {
		if len(scorers) == 0 {
			return 0.0
		}
		total := 0.0
		for _, s := range scorers {
			total += s(t)
		}
		return total / float64(len(scorers))
	}
}

// ScoreTrajectories applies a scorer to a slice of trajectories in place,
// setting each trajectory's Score field. Returns the average score.
func ScoreTrajectories(trajectories []Trajectory, scorer Scorer) float64 {
	if len(trajectories) == 0 {
		return 0.0
	}
	total := 0.0
	for i := range trajectories {
		trajectories[i].Score = scorer(trajectories[i])
		total += trajectories[i].Score
	}
	return total / float64(len(trajectories))
}

// PartitionTrajectories splits trajectories into successes and failures
// based on a score threshold.
func PartitionTrajectories(trajectories []Trajectory, threshold float64) (successes, failures []Trajectory) {
	for _, t := range trajectories {
		if t.IsSuccess(threshold) {
			successes = append(successes, t)
		} else {
			failures = append(failures, t)
		}
	}
	return
}
