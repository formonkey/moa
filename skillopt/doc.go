// Package skillopt provides a text-space optimizer for training reusable
// natural-language skills that improve frozen LLM agent performance.
//
// Inspired by Microsoft's SkillOpt, this package treats a natural language
// document (the "skill") as a trainable external state. Instead of fine-tuning
// model weights, it evolves the skill document through trajectory-driven edits,
// validation-gated updates, and deployable best_skill.md artifacts.
//
// The optimization loop mirrors a standard ML training cycle:
//
//  1. Forward Pass: The frozen target agent executes tasks using the current skill.
//  2. Scoring: Each trajectory is scored against expected outcomes.
//  3. Backward Pass: An optimizer LLM analyzes trajectories and proposes structured edits.
//  4. Validation Gate: Edits are only accepted if they improve validation score.
//  5. Checkpoint: The best skill is persisted when improvement is detected.
//
// Key features:
//   - Textual Learning Rate: Bounded edit budget prevents destructive rewrites.
//   - Validation Gate: Ensures monotonic improvement on held-out data.
//   - Epochs & Batches: Full ML-style training loop with configurable hyperparameters.
//   - Zero Inference Overhead: The resulting skill.md is a static file.
//   - Multi-Provider: Works with any model.LLM provider in moa.
//
// Quick start:
//
//	opt, _ := skillopt.New(skillopt.Config{
//	    TargetModel:    gemini.New("gemini-2.5-flash"),
//	    OptimizerModel: gemini.New("gemini-2.5-pro"),
//	    Epochs:         3,
//	    BatchSize:      5,
//	    LearningRate:   3,
//	    InitialSkill:   "You are a Go expert.",
//	    OutputDir:      "./skills_output",
//	})
//
//	result, _ := opt.Train(ctx, dataset)
//	fmt.Println(result.BestSkill)
package skillopt
