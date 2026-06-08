package skillopt

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
)

// Reflector uses an optimizer LLM to analyze scored trajectories and
// propose structured edits to the skill document.
//
// This implements the "backward pass" of the text-space optimization loop.
type Reflector struct {
	model    model.LLM
	maxEdits int // Bounded by textual learning rate.
}

// NewReflector creates a new Reflector with the given optimizer model
// and maximum edits per reflection (learning rate).
func NewReflector(optimizerModel model.LLM, maxEdits int) *Reflector {
	if maxEdits <= 0 {
		maxEdits = 3
	}
	return &Reflector{
		model:    optimizerModel,
		maxEdits: maxEdits,
	}
}

// Reflect analyzes a batch of scored trajectories and proposes edits to
// the current skill document. The returned edits are bounded by maxEdits.
func (r *Reflector) Reflect(ctx context.Context, currentSkill string, trajectories []Trajectory) ([]Edit, error) {
	prompt := r.buildPrompt(currentSkill, trajectories)

	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText(prompt, "user"),
		},
		SystemInstruction: genai.NewContentFromText(reflectorSystemPrompt, "user"),
		Config: &genai.GenerateContentConfig{
			Temperature: genai.Ptr(float32(0.3)), // Low temp for precise edits
		},
	}

	var responseText strings.Builder
	for resp, err := range r.model.GenerateContent(ctx, req, false) {
		if err != nil {
			return nil, fmt.Errorf("reflector LLM error: %w", err)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}
	}

	text := responseText.String()
	if text == "" {
		return nil, fmt.Errorf("reflector returned empty response")
	}

	edits, err := ParseEditsJSON(text)
	if err != nil {
		return nil, fmt.Errorf("parsing reflector response: %w", err)
	}

	// Enforce learning rate cap
	if len(edits) > r.maxEdits {
		edits = edits[:r.maxEdits]
	}

	return edits, nil
}

// buildPrompt constructs the reflection prompt with the current skill
// and partitioned trajectory summaries.
func (r *Reflector) buildPrompt(currentSkill string, trajectories []Trajectory) string {
	successes, failures := PartitionTrajectories(trajectories, 0.8)

	var b strings.Builder

	b.WriteString("## Current Skill Document\n\n")
	if currentSkill == "" {
		b.WriteString("(empty — no skill document yet)\n")
	} else {
		b.WriteString("```\n")
		b.WriteString(currentSkill)
		b.WriteString("\n```\n")
	}

	b.WriteString(fmt.Sprintf("\n## Trajectory Analysis (%d total, %d success, %d failure)\n\n",
		len(trajectories), len(successes), len(failures)))

	if len(successes) > 0 {
		b.WriteString("### Successful Trajectories (score >= 0.8)\n\n")
		for i, t := range successes {
			if i >= 5 { // Cap at 5 to avoid context overflow
				b.WriteString(fmt.Sprintf("... and %d more successes\n", len(successes)-5))
				break
			}
			b.WriteString(t.Summary())
			b.WriteString("\n---\n")
		}
	}

	if len(failures) > 0 {
		b.WriteString("\n### Failed Trajectories (score < 0.8)\n\n")
		for i, t := range failures {
			if i >= 5 { // Cap at 5
				b.WriteString(fmt.Sprintf("... and %d more failures\n", len(failures)-5))
				break
			}
			b.WriteString(t.Summary())
			b.WriteString("\n---\n")
		}
	}

	b.WriteString(fmt.Sprintf("\n## Edit Budget\n\nYou may propose at most **%d** edits.\n", r.maxEdits))

	return b.String()
}

const reflectorSystemPrompt = `You are a skill optimizer. Your job is to analyze agent trajectories and propose precise edits to a "skill document" — a natural language instruction set that guides a frozen LLM agent.

Your goal: improve the agent's task success rate by editing the skill document.

## Analysis Strategy

1. STUDY the failed trajectories to identify patterns: wrong approach, missing context, incorrect tool usage, hallucinations, etc.
2. STUDY the successful trajectories to identify what works well: good patterns, effective instructions, useful examples.
3. PROPOSE targeted edits that address failure patterns while preserving success patterns.

## Edit Format

Respond with a JSON array of edits. Each edit has:
- "type": "add" | "replace" | "delete"
- "section": a label for the section being modified
- "target": (for replace/delete) the exact text to find in the current skill
- "content": (for add/replace) the new text
- "rationale": brief explanation of why this edit will help

## Rules

- Be PRECISE with "target" — it must exactly match text in the current skill.
- Keep edits SMALL and FOCUSED. One concept per edit.
- Prefer "replace" over "delete + add" when modifying existing content.
- For new content, use "add" to append instructions, examples, or guidelines.
- DO NOT propose edits that contradict each other.
- Each edit should be independently valuable (not dependent on other edits).
- Keep the skill concise — avoid verbose instructions.

## Response Format

Respond ONLY with the JSON array. No other text.

` + "```json\n" + `[
  {"type": "add", "section": "examples", "content": "...", "rationale": "..."},
  {"type": "replace", "section": "instructions", "target": "...", "content": "...", "rationale": "..."}
]
` + "```"
