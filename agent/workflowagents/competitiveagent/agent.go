// Package competitiveagent provides a democratic workflow where multiple workers
// produce solutions in parallel, then ALL remaining agents vote to elect the best.
//
// Flow: N workers execute in parallel → all voters rank → majority wins → final output
//
// There is no master or judge. The winner is determined purely by democratic vote.
package competitiveagent

import (
	"fmt"
	"iter"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/session"
)

// Config for a competitive workflow agent.
type Config struct {
	Name        string
	Description string
	// Workers are the agents that produce competing solutions.
	// Each worker runs in parallel on the same task.
	Workers []agent.Agent
	// Voters are all other agents that evaluate and vote on the outputs.
	// If empty, all workers vote on each other's outputs (self-excluding).
	Voters []agent.Agent
}

// New creates a competitive workflow agent with democratic voting.
func New(cfg Config) (agent.Agent, error) {
	if len(cfg.Workers) < 2 {
		return nil, fmt.Errorf("competitiveagent: at least 2 workers are required")
	}

	return agent.New(agent.Config{
		Name:        cfg.Name,
		Description: cfg.Description,
		SubAgents:   buildSubAgents(cfg.Workers, cfg.Voters),
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				bus := ctx.EventBus()

				// ── Phase 1: All workers produce solutions in parallel ──
				type workerResult struct {
					name   string
					output string
				}
				results := make([]workerResult, 0, len(cfg.Workers))
				var mu sync.Mutex
				var wg sync.WaitGroup

				for _, worker := range cfg.Workers {
					wg.Add(1)
					go func(w agent.Agent) {
						defer wg.Done()

						bus.Emit(agent.BusEvent{
							Type:    agent.EventAgentSwitch,
							Agent:   cfg.Name,
							Payload: fmt.Sprintf("worker:%s", w.Name()),
						})

						childCtx := agent.NewInvocationContext(agent.InvocationContextParams{
							Ctx:          ctx,
							Agent:        w,
							Session:      ctx.Session(),
							InvocationID: ctx.InvocationID(),
							Branch:       fmt.Sprintf("%s.worker.%s", cfg.Name, w.Name()),
							UserContent:  ctx.UserContent(),
							RunConfig:    ctx.RunConfig(),
							ParentMap:    ctx.ParentMap(),
							EventBus:     ctx.EventBus(),
						})

						var output string
						var workerErr error
						for event, err := range w.Run(childCtx) {
							if err != nil {
								workerErr = err
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

						if workerErr != nil {
							slog.Warn("competitiveagent worker failed", "worker", w.Name(), "error", workerErr)
							return // skip this worker's result
						}

						mu.Lock()
						results = append(results, workerResult{name: w.Name(), output: output})
						mu.Unlock()
					}(worker)
				}
				wg.Wait()

				if len(results) == 0 {
					yield(nil, fmt.Errorf("competitiveagent: all workers failed"))
					return
				}

				// Build the ballot: all implementations for voters to see
				var ballot strings.Builder
				for i, r := range results {
					ballot.WriteString(fmt.Sprintf("\n=== SOLUTION %d (by %s) ===\n%s\n", i+1, r.name, r.output))
				}
				ballotText := ballot.String()

				// ── Phase 2: Democratic voting ──
				// Determine voters: explicit Voters, or all workers vote on each other
				voters := cfg.Voters
				if len(voters) == 0 {
					voters = cfg.Workers
				}

				// Tally: map[worker_name] → vote count
				tally := make(map[string]int)
				for _, r := range results {
					tally[r.name] = 0
				}

				for _, voter := range voters {
					bus.Emit(agent.BusEvent{
						Type:    agent.EventAgentSwitch,
						Agent:   cfg.Name,
						Payload: fmt.Sprintf("voter:%s", voter.Name()),
					})

					// Inject ballot into session state for the voter
					_ = ctx.Session().State().Set("competitive_ballot", ballotText)

					// Build candidate names for voting instruction
					var candidateNames []string
					for _, r := range results {
						candidateNames = append(candidateNames, r.name)
					}

					voteInstruction := fmt.Sprintf(
						"You are voting on the best solution. Review all solutions below and respond with EXACTLY the name of the best one.\n\nCandidates: %s\n\n%s\n\nRespond with ONLY the name of the winning solution.",
						strings.Join(candidateNames, ", "),
						ballotText,
					)

					// Pass the ballot as UserContent so the voter actually sees it
					voteContent := &genai.Content{
						Role:  "user",
						Parts: []*genai.Part{{Text: voteInstruction}},
					}

					voterCtx := agent.NewInvocationContext(agent.InvocationContextParams{
						Ctx:          ctx,
						Agent:        voter,
						Session:      ctx.Session(),
						InvocationID: ctx.InvocationID(),
						Branch:       fmt.Sprintf("%s.voter.%s", cfg.Name, voter.Name()),
						UserContent:  voteContent,
						RunConfig:    ctx.RunConfig(),
						ParentMap:    ctx.ParentMap(),
						EventBus:     ctx.EventBus(),
					})

					var voteText string
					var voterErr error
					for event, err := range voter.Run(voterCtx) {
						if err != nil {
							voterErr = err
							break
						}
						if event != nil && event.Content != nil {
							for _, p := range event.Content.Parts {
								if p.Text != "" {
									voteText += p.Text
								}
							}
						}
					}

					if voterErr != nil {
						slog.Warn("competitiveagent voter failed", "voter", voter.Name(), "error", voterErr)
						continue // skip this voter's ballot (abstention)
					}

					// Count the vote — fuzzy match against candidate names
					voteText = strings.TrimSpace(voteText)
					for _, r := range results {
						if strings.Contains(strings.ToLower(voteText), strings.ToLower(r.name)) {
							tally[r.name]++
							break
						}
					}
				}

				// ── Phase 3: Determine winner by majority ──
				type candidate struct {
					name  string
					votes int
				}
				var candidates []candidate
				for name, votes := range tally {
					candidates = append(candidates, candidate{name, votes})
				}
				sort.Slice(candidates, func(i, j int) bool {
					return candidates[i].votes > candidates[j].votes
				})

				winnerName := candidates[0].name
				var winnerOutput string
				for _, r := range results {
					if r.name == winnerName {
						winnerOutput = r.output
						break
					}
				}

				// Build the final summary
				var summary strings.Builder
				summary.WriteString(fmt.Sprintf("🏆 WINNER: %s (%d/%d votes)\n\n", winnerName, candidates[0].votes, len(voters)))
				summary.WriteString("Vote tally:\n")
				for _, c := range candidates {
					summary.WriteString(fmt.Sprintf("  %s: %d votes\n", c.name, c.votes))
				}
				summary.WriteString(fmt.Sprintf("\n--- Winning Solution ---\n%s", winnerOutput))

				// Emit the winner as the final event
				finalEvent := session.NewEvent(ctx.InvocationID())
				finalEvent.Author = cfg.Name
				finalEvent.Branch = ctx.Branch()
				finalEvent.LLMResponse.Content = &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: summary.String()}},
				}
				yield(finalEvent, nil)
			}
		},
	})
}

func buildSubAgents(workers, voters []agent.Agent) []agent.Agent {
	seen := make(map[string]bool)
	var all []agent.Agent
	for _, a := range workers {
		if !seen[a.Name()] {
			all = append(all, a)
			seen[a.Name()] = true
		}
	}
	for _, a := range voters {
		if !seen[a.Name()] {
			all = append(all, a)
			seen[a.Name()] = true
		}
	}
	return all
}
