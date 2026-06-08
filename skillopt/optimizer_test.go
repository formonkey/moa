package skillopt_test

import (
	"fmt"
	"testing"

	"github.com/formonkey/moa/skillopt"
)

// --- Edit tests ---

func TestApplyEdits_Add(t *testing.T) {
	skill := "You are a helpful assistant."
	edits := []skillopt.Edit{
		{Type: skillopt.EditAdd, Section: "examples", Content: "Always be concise.", Rationale: "test"},
	}

	result, applied, skipped := skillopt.ApplyEdits(skill, edits)
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(applied))
	}
	if len(skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(skipped))
	}
	if result == skill {
		t.Fatal("expected skill to be modified")
	}
	if !contains(result, "Always be concise.") {
		t.Fatalf("expected result to contain added content, got: %s", result)
	}
}

func TestApplyEdits_Replace(t *testing.T) {
	skill := "You are a helpful assistant. Answer in English."
	edits := []skillopt.Edit{
		{Type: skillopt.EditReplace, Section: "lang", Target: "Answer in English.", Content: "Answer in Spanish.", Rationale: "test"},
	}

	result, applied, skipped := skillopt.ApplyEdits(skill, edits)
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(applied))
	}
	if len(skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(skipped))
	}
	if !contains(result, "Answer in Spanish.") {
		t.Fatalf("expected result to contain replaced content, got: %s", result)
	}
	if contains(result, "Answer in English.") {
		t.Fatal("expected original text to be replaced")
	}
}

func TestApplyEdits_Delete(t *testing.T) {
	skill := "You are a helpful assistant. Never use code blocks."
	edits := []skillopt.Edit{
		{Type: skillopt.EditDelete, Section: "rules", Target: " Never use code blocks.", Rationale: "test"},
	}

	result, applied, skipped := skillopt.ApplyEdits(skill, edits)
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(applied))
	}
	if len(skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(skipped))
	}
	if contains(result, "Never use code blocks.") {
		t.Fatal("expected deleted text to be removed")
	}
}

func TestApplyEdits_TargetNotFound(t *testing.T) {
	skill := "You are a helpful assistant."
	edits := []skillopt.Edit{
		{Type: skillopt.EditReplace, Section: "x", Target: "nonexistent text", Content: "new", Rationale: "test"},
	}

	_, applied, skipped := skillopt.ApplyEdits(skill, edits)
	if len(applied) != 0 {
		t.Fatalf("expected 0 applied, got %d", len(applied))
	}
	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped, got %d", len(skipped))
	}
}

func TestApplyEdits_MultipleEdits(t *testing.T) {
	skill := "You are a Go expert. Use verbose explanations."
	edits := []skillopt.Edit{
		{Type: skillopt.EditReplace, Section: "style", Target: "verbose", Content: "concise", Rationale: "brevity"},
		{Type: skillopt.EditAdd, Section: "examples", Content: "Example: fmt.Println(\"hello\")", Rationale: "show code"},
	}

	result, applied, skipped := skillopt.ApplyEdits(skill, edits)
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied, got %d", len(applied))
	}
	if len(skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(skipped))
	}
	if !contains(result, "concise") {
		t.Fatal("expected concise")
	}
	if !contains(result, "fmt.Println") {
		t.Fatal("expected code example")
	}
}

// --- ParseEditsJSON tests ---

func TestParseEditsJSON(t *testing.T) {
	input := `Here are my proposed edits:
` + "```json" + `
[
  {"type": "add", "section": "tips", "content": "Be concise.", "rationale": "shorter is better"},
  {"type": "replace", "section": "style", "target": "verbose", "content": "concise", "rationale": "brevity"}
]
` + "```"

	edits, err := skillopt.ParseEditsJSON(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}
	if edits[0].Type != skillopt.EditAdd {
		t.Fatalf("expected add, got %s", edits[0].Type)
	}
	if edits[1].Type != skillopt.EditReplace {
		t.Fatalf("expected replace, got %s", edits[1].Type)
	}
}

func TestParseEditsJSON_InvalidMissingType(t *testing.T) {
	input := `[{"section": "tips", "content": "Be concise."}]`
	_, err := skillopt.ParseEditsJSON(input)
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestParseEditsJSON_NoJSON(t *testing.T) {
	input := "No JSON here, just text."
	_, err := skillopt.ParseEditsJSON(input)
	if err == nil {
		t.Fatal("expected error for no JSON")
	}
}

// --- Scorer tests ---

func TestExactMatchScorer(t *testing.T) {
	scorer := skillopt.ExactMatchScorer()

	traj := skillopt.Trajectory{Output: " net/http ", Expected: []string{"net/http"}}
	if s := scorer(traj); s != 1.0 {
		t.Fatalf("expected 1.0, got %f", s)
	}

	traj2 := skillopt.Trajectory{Output: "net/http package", Expected: []string{"net/http"}}
	if s := scorer(traj2); s != 0.0 {
		t.Fatalf("expected 0.0, got %f", s)
	}
}

func TestContainsScorer(t *testing.T) {
	scorer := skillopt.ContainsScorer()

	traj := skillopt.Trajectory{Output: "The answer is net/http package.", Expected: []string{"net/http"}}
	if s := scorer(traj); s != 1.0 {
		t.Fatalf("expected 1.0, got %f", s)
	}

	traj2 := skillopt.Trajectory{Output: "I don't know", Expected: []string{"net/http"}}
	if s := scorer(traj2); s != 0.0 {
		t.Fatalf("expected 0.0, got %f", s)
	}
}

func TestFuzzyContainsScorer(t *testing.T) {
	scorer := skillopt.FuzzyContainsScorer()

	traj := skillopt.Trajectory{
		Output:   "Go uses net/http for web and encoding/json for data.",
		Expected: []string{"net/http", "encoding/json", "fmt"},
	}
	score := scorer(traj)
	// 2 out of 3 expected found
	expected := 2.0 / 3.0
	if diff := score - expected; diff > 0.01 || diff < -0.01 {
		t.Fatalf("expected ~%.3f, got %f", expected, score)
	}
}

func TestScoreTrajectories(t *testing.T) {
	trajectories := []skillopt.Trajectory{
		{Output: "yes", Expected: []string{"yes"}},
		{Output: "no", Expected: []string{"yes"}},
	}
	avg := skillopt.ScoreTrajectories(trajectories, skillopt.ExactMatchScorer())
	if avg != 0.5 {
		t.Fatalf("expected 0.5, got %f", avg)
	}
	if trajectories[0].Score != 1.0 {
		t.Fatal("expected first score = 1.0")
	}
	if trajectories[1].Score != 0.0 {
		t.Fatal("expected second score = 0.0")
	}
}

// --- Dataset tests ---

func TestFromSlice(t *testing.T) {
	tasks := make([]skillopt.Task, 20)
	for i := range tasks {
		tasks[i] = skillopt.Task{ID: fmt.Sprintf("t%d", i), Input: "q", Expected: []string{"a"}}
	}

	ds := skillopt.FromSlice(tasks, 0.2)
	if len(ds.Val) < 1 {
		t.Fatal("expected at least 1 val task")
	}
	if len(ds.Train) < 1 {
		t.Fatal("expected at least 1 train task")
	}
	if len(ds.Train)+len(ds.Val) != 20 {
		t.Fatalf("expected 20 total, got %d", len(ds.Train)+len(ds.Val))
	}
}

func TestDataset_Batches(t *testing.T) {
	tasks := make([]skillopt.Task, 12)
	for i := range tasks {
		tasks[i] = skillopt.Task{ID: fmt.Sprintf("t%d", i)}
	}

	ds := &skillopt.Dataset{Train: tasks, Val: tasks[:2]}
	batches := ds.Batches(5)
	if len(batches) != 3 { // 5 + 5 + 2
		t.Fatalf("expected 3 batches, got %d", len(batches))
	}
	if len(batches[0]) != 5 {
		t.Fatalf("expected first batch of 5, got %d", len(batches[0]))
	}
	if len(batches[2]) != 2 {
		t.Fatalf("expected last batch of 2, got %d", len(batches[2]))
	}
}

func TestDataset_Validate(t *testing.T) {
	ds := &skillopt.Dataset{}
	if err := ds.Validate(); err == nil {
		t.Fatal("expected error for empty dataset")
	}

	ds.Train = []skillopt.Task{{ID: "1"}}
	if err := ds.Validate(); err == nil {
		t.Fatal("expected error for empty val")
	}

	ds.Val = []skillopt.Task{{ID: "2"}}
	if err := ds.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Metrics tests ---

func TestTrainHistory_BestEpoch(t *testing.T) {
	history := skillopt.TrainHistory{
		{Epoch: 0, ValScore: 0.5},
		{Epoch: 1, ValScore: 0.8},
		{Epoch: 2, ValScore: 0.7},
	}

	best, idx := history.BestEpoch()
	if idx != 1 {
		t.Fatalf("expected best at index 1, got %d", idx)
	}
	if best.ValScore != 0.8 {
		t.Fatalf("expected val score 0.8, got %f", best.ValScore)
	}
}

func TestTrainHistory_BestEpoch_Empty(t *testing.T) {
	history := skillopt.TrainHistory{}
	_, idx := history.BestEpoch()
	if idx != -1 {
		t.Fatalf("expected -1 for empty history, got %d", idx)
	}
}

// --- Trajectory tests ---

func TestPartitionTrajectories(t *testing.T) {
	trajectories := []skillopt.Trajectory{
		{TaskID: "a", Score: 1.0},
		{TaskID: "b", Score: 0.5},
		{TaskID: "c", Score: 0.9},
		{TaskID: "d", Score: 0.0},
	}

	successes, failures := skillopt.PartitionTrajectories(trajectories, 0.8)
	if len(successes) != 2 {
		t.Fatalf("expected 2 successes, got %d", len(successes))
	}
	if len(failures) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(failures))
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Suppress unused import warning - fmt is used in TestFromSlice
var _ = fmt.Sprintf
