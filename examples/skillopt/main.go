// Example skillopt demonstrates how to use the SkillOpt text-space optimizer
// to train a natural-language skill for an LLM agent.
//
// Usage:
//
//	export GOOGLE_API_KEY="your-key"
//	go run ./examples/skillopt/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/formonkey/moa/model/gemini"
	"github.com/formonkey/moa/skillopt"
)

func main() {
	ctx := context.Background()

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		log.Fatal("Set GOOGLE_API_KEY environment variable")
	}

	// Create a dataset of Go knowledge questions
	dataset := skillopt.FromSlice([]skillopt.Task{
		{ID: "1", Input: "What is Go's zero value for an int?", Expected: []string{"0"}},
		{ID: "2", Input: "What package handles HTTP in Go?", Expected: []string{"net/http"}},
		{ID: "3", Input: "What is the keyword to create a goroutine?", Expected: []string{"go"}},
		{ID: "4", Input: "What built-in function appends to a slice?", Expected: []string{"append"}},
		{ID: "5", Input: "What is the zero value of a string in Go?", Expected: []string{"\"\"", "empty string", ""}},
		{ID: "6", Input: "What keyword is used for error handling patterns?", Expected: []string{"if err != nil", "if", "error"}},
		{ID: "7", Input: "What is Go's package manager tool?", Expected: []string{"go mod", "modules"}},
		{ID: "8", Input: "What function starts a test in Go?", Expected: []string{"func Test", "Test"}},
		{ID: "9", Input: "What interface has zero methods in Go?", Expected: []string{"any", "interface{}"}},
		{ID: "10", Input: "What is the keyword to define a struct?", Expected: []string{"type", "struct"}},
		{ID: "11", Input: "How do you declare a constant in Go?", Expected: []string{"const"}},
		{ID: "12", Input: "What channel direction keyword sends data?", Expected: []string{"chan<-", "chan"}},
		{ID: "13", Input: "What is the name of Go's formatter tool?", Expected: []string{"gofmt", "go fmt"}},
		{ID: "14", Input: "What keyword creates a new type alias?", Expected: []string{"type"}},
		{ID: "15", Input: "What function prints with a newline?", Expected: []string{"fmt.Println", "Println"}},
	}, 0.2)

	targetModel, err := gemini.NewClient(ctx, apiKey, "gemini-2.5-flash")
	if err != nil {
		log.Fatalf("Failed to create target model: %v", err)
	}
	optimizerModel, err := gemini.NewClient(ctx, apiKey, "gemini-2.5-pro")
	if err != nil {
		log.Fatalf("Failed to create optimizer model: %v", err)
	}

	// Configure the optimizer
	opt, err := skillopt.New(skillopt.Config{
		TargetModel:    targetModel,
		OptimizerModel: optimizerModel, // More powerful for reflection
		Epochs:         3,
		BatchSize:      5,
		LearningRate:   3,
		InitialSkill:   "You are a Go programming expert. Answer questions concisely with just the answer, no explanations.",
		OutputDir:      "./skills_output",
		Scorer:         skillopt.ContainsScorer(),
		OnEpochStart: func(epoch int) {
			fmt.Printf("\n🔄 Starting epoch %d...\n", epoch)
		},
		OnBatchDone: func(epoch, batch int, m skillopt.BatchMetrics) {
			fmt.Printf("  📊 Batch %d: avg=%.2f min=%.2f max=%.2f tasks=%d\n",
				batch, m.AvgScore, m.MinScore, m.MaxScore, m.TasksDone)
		},
		OnEditApplied: func(edit skillopt.Edit) {
			fmt.Printf("  ✅ %s\n", edit)
		},
		OnEditRejected: func(edit skillopt.Edit, reason string) {
			fmt.Printf("  ❌ %s — %s\n", edit, reason)
		},
		OnEpochEnd: func(epoch int, m skillopt.EpochMetrics) {
			fmt.Printf("📈 Epoch %d done: train=%.2f val=%.2f edits=+%d/-%d skill=%d chars\n",
				epoch, m.TrainScore, m.ValScore, m.EditsApplied, m.EditsRejected, m.SkillLen)
		},
	})
	if err != nil {
		log.Fatalf("Failed to create optimizer: %v", err)
	}

	// Run training
	fmt.Println("🚀 Starting SkillOpt training...")
	fmt.Printf("   Dataset: %d train, %d val\n", len(dataset.Train), len(dataset.Val))
	fmt.Println()

	result, err := opt.Train(ctx, *dataset)
	if err != nil {
		log.Fatalf("Training failed: %v", err)
	}

	// Print results
	fmt.Println("\n" + result.History.Summary())
	fmt.Printf("⏱️  Total time: %s\n", result.Duration.Round(100*time.Millisecond))
	fmt.Printf("✏️  Edits: %d applied, %d rejected\n", result.TotalEdits, result.RejectedEdits)
	fmt.Printf("🏆 Best epoch: %d (val=%.3f)\n", result.BestEpoch, result.BestScore)
	fmt.Println("\n📄 Best Skill:")
	fmt.Println("─────────────────────────────")
	fmt.Println(result.BestSkill)
	fmt.Println("─────────────────────────────")
}
