// Package debateagent provides a consensus-driven workflow where agents debate
// a topic across multiple rounds until consensus emerges.
//
// Flow: All debaters produce initial positions → N rounds of rebuttals →
// final consensus synthesis (no master/judge — the collective output IS the result)
//
// Consensus is detected when debaters start agreeing, or after max rounds.
package debateagent

import (
	"fmt"
	"iter"
	"log/slog"
	"strings"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
)

// Config for a consensus debate workflow agent.
type Config struct {
	Name        string
	Description string
	// Debaters are the agents that participate in the debate.
	// Each one produces an initial position and then responds to others' arguments.
	Debaters []agent.Agent
	// MaxRounds is the maximum number of debate rounds. Default: 3.
	MaxRounds int
}

// New creates a consensus debate workflow agent.
func New(cfg Config) (agent.Agent, error) {
	if len(cfg.Debaters) < 2 {
		return nil, fmt.Errorf("debateagent: at least 2 debaters are required")
	}
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 3
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		SubAgents:   cfg.Debaters,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				bus := ctx.EventBus()

				// Track each debater's position across rounds
				positions := make(map[string]string) // debater name → latest position

				// ── Phase 1: Initial positions ──
				for _, debater := range cfg.Debaters {
					bus.Emit(agent.BusEvent{
						Type:    agent.EventAgentSwitch,
						Agent:   cfg.Name,
						Payload: fmt.Sprintf("round:0:%s", debater.Name()),
					})

					childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
						Ctx:          ctx,
						Agent:        debater,
						Session:      ctx.Session(),
						InvocationID: ctx.InvocationID(),
						Branch:       fmt.Sprintf("%s.round0.%s", cfg.Name, debater.Name()),
						UserContent:  ctx.UserContent(),
						RunConfig:    ctx.RunConfig(),
						ParentMap:    ctx.ParentMap(),
						EventBus:     ctx.EventBus(),
					})

					var output string
					for event, err := range debater.Run(childCtx) {
						if err != nil {
							yield(nil, fmt.Errorf("debateagent: debater %s failed in initial round: %w", debater.Name(), err))
							return
						}
						if event != nil && event.Content != nil {
							for _, p := range event.Content.Parts {
								if p.Text != "" {
									output += p.Text
								}
							}
						}
					}
					positions[debater.Name()] = output
				}

				// ── Phase 2: Debate rounds ──
				for round := 1; round <= cfg.MaxRounds; round++ {
					if ctx.Ended() {
						return
					}

					// Build the transcript of all current positions
					var transcript strings.Builder
					for name, pos := range positions {
						transcript.WriteString(fmt.Sprintf("\n=== Position by %s ===\n%s\n", name, pos))
					}
					transcriptText := transcript.String()

					// Each debater reads others' positions and responds
					newPositions := make(map[string]string)

					for _, debater := range cfg.Debaters {
						bus.Emit(agent.BusEvent{
							Type:    agent.EventAgentSwitch,
							Agent:   cfg.Name,
							Payload: fmt.Sprintf("round:%d:%s", round, debater.Name()),
						})

						// Build debate instruction as user content
						debatePrompt := fmt.Sprintf(
							"This is round %d of a debate. Read all positions below and respond with your updated position. "+
								"If you agree with another debater, say so explicitly. "+
								"Focus on finding common ground and building consensus.\n\n%s",
							round, transcriptText)

						debateContent := &genai.Content{
							Role:  "user",
							Parts: []*genai.Part{{Text: debatePrompt}},
						}

						childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
							Ctx:          ctx,
							Agent:        debater,
							Session:      ctx.Session(),
							InvocationID: ctx.InvocationID(),
							Branch:       fmt.Sprintf("%s.round%d.%s", cfg.Name, round, debater.Name()),
							UserContent:  debateContent,
							RunConfig:    ctx.RunConfig(),
							ParentMap:    ctx.ParentMap(),
							EventBus:     ctx.EventBus(),
						})

						var output string
						var debaterErr error
						for event, err := range debater.Run(childCtx) {
							if err != nil {
								debaterErr = err
								break
							}
							if event != nil && event.Content != nil {
								for _, p := range event.Content.Parts {
									if p.Text != "" {
										output += p.Text
									}
								}
							}
						}
						if debaterErr != nil {
							slog.Warn("debateagent debater failed", "debater", debater.Name(), "round", round, "error", debaterErr)
							// Keep previous position on error
							continue
						}
						newPositions[debater.Name()] = output
					}

					// Update positions
					for name, pos := range newPositions {
						positions[name] = pos
					}

					// Check for early consensus: if all positions are very similar
					if detectConsensus(positions) {
						break
					}
				}

				// ── Phase 3: Synthesize consensus ──
				var consensus strings.Builder
				consensus.WriteString("=== DEBATE CONSENSUS ===\n\n")
				consensus.WriteString(fmt.Sprintf("After %d rounds with %d debaters:\n\n",
					cfg.MaxRounds, len(cfg.Debaters)))

				for _, debater := range cfg.Debaters {
					pos := positions[debater.Name()]
					consensus.WriteString(fmt.Sprintf("── Final position by %s ──\n%s\n\n", debater.Name(), pos))
				}

				// Emit the consensus as the final event
				finalEvent := session.NewEvent(ctx.InvocationID())
				finalEvent.Author = cfg.Name
				finalEvent.Branch = ctx.Branch()
				finalEvent.LLMResponse.Content = &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: consensus.String()}},
				}
				yield(finalEvent, nil)
			}
		},
	})
}

// detectConsensus checks if debaters are converging by looking for agreement signals.
// Returns true if positions show strong overlap, indicating consensus.
func detectConsensus(positions map[string]string) bool {
	if len(positions) < 2 {
		return true
	}

	// Simple heuristic: check if debaters explicitly signal agreement
	agreementSignals := []string{
		"i agree",
		"consensus",
		"we all agree",
		"aligned",
		"same conclusion",
		"de acuerdo",
		"consenso",
	}

	agreementCount := 0
	for _, pos := range positions {
		lower := strings.ToLower(pos)
		for _, signal := range agreementSignals {
			if strings.Contains(lower, signal) {
				agreementCount++
				break
			}
		}
	}

	// Consensus if majority signals agreement
	return agreementCount >= len(positions)-1
}
