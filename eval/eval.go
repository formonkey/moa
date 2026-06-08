// Package eval provides a testing framework for evaluating agent behavior.
//
// It allows defining test cases with expected outcomes and running them against
// agents to validate correctness, regression, and quality. Supports exact match,
// contains, regex, and custom assertion functions.
//
// Usage:
//
//	suite := eval.NewSuite("MyAgent Tests")
//	suite.Add(eval.TestCase{
//	    Name:   "greeting",
//	    Input:  "Hello",
//	    Assert: eval.Contains("hello"),
//	})
//	results := suite.Run(ctx, myAgent, sessionService)
package eval

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/formonkey/moa/agent"
	"github.com/formonkey/moa/runner"
	"github.com/formonkey/moa/session"
)

// TestCase defines a single evaluation scenario.
type TestCase struct {
	// Name identifies this test case.
	Name string
	// Input is the user message to send to the agent.
	Input string
	// Assert is the assertion function to validate the agent's response.
	Assert AssertFunc
	// Timeout for this test case. Default: 30s.
	Timeout time.Duration
	// InitialState is optional state to inject before running.
	InitialState map[string]any
}

// AssertFunc validates an agent's response text. Returns nil on success, error on failure.
type AssertFunc func(response string) error

// Result is the outcome of a single test case.
type Result struct {
	Name     string
	Passed   bool
	Response string
	Error    error
	Duration time.Duration
}

// Suite is a collection of test cases to run against an agent.
type Suite struct {
	Name  string
	Cases []TestCase
}

// NewSuite creates a new evaluation suite.
func NewSuite(name string) *Suite {
	return &Suite{Name: name}
}

// Add appends a test case to the suite.
func (s *Suite) Add(tc TestCase) {
	s.Cases = append(s.Cases, tc)
}

// SuiteResults holds the results of running all test cases in a suite.
type SuiteResults struct {
	SuiteName string
	Results   []Result
	Passed    int
	Failed    int
	Total     int
	Duration  time.Duration
}

// Run executes all test cases against the given agent and session service.
func (s *Suite) Run(ctx context.Context, rootAgent agent.Agent, sessService session.Service) *SuiteResults {
	start := time.Now()
	results := &SuiteResults{
		SuiteName: s.Name,
		Total:     len(s.Cases),
	}

	for _, tc := range s.Cases {
		result := runTestCase(ctx, rootAgent, sessService, tc)
		results.Results = append(results.Results, result)
		if result.Passed {
			results.Passed++
		} else {
			results.Failed++
		}
	}

	results.Duration = time.Since(start)
	return results
}

// Summary returns a human-readable summary of the suite results.
func (r *SuiteResults) Summary() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n=== %s ===\n", r.SuiteName))
	b.WriteString(fmt.Sprintf("Total: %d | Passed: %d | Failed: %d | Duration: %s\n\n",
		r.Total, r.Passed, r.Failed, r.Duration.Round(time.Millisecond)))

	for _, res := range r.Results {
		status := "✅ PASS"
		if !res.Passed {
			status = "❌ FAIL"
		}
		b.WriteString(fmt.Sprintf("  %s  %s (%s)\n", status, res.Name, res.Duration.Round(time.Millisecond)))
		if !res.Passed && res.Error != nil {
			b.WriteString(fmt.Sprintf("         Error: %v\n", res.Error))
			if res.Response != "" {
				// Truncate long responses
				resp := res.Response
				if len(resp) > 200 {
					resp = resp[:200] + "..."
				}
				b.WriteString(fmt.Sprintf("         Got: %q\n", resp))
			}
		}
	}
	return b.String()
}

func runTestCase(ctx context.Context, rootAgent agent.Agent, sessService session.Service, tc TestCase) Result {
	start := time.Now()

	timeout := tc.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create a fresh session for each test
	sessionID := fmt.Sprintf("eval-%s-%d", tc.Name, time.Now().UnixNano())
	r, err := runner.New(runner.Config{
		AppName:           "eval",
		Agent:             rootAgent,
		SessionService:    sessService,
		AutoCreateSession: true,
	})
	if err != nil {
		return Result{Name: tc.Name, Passed: false, Error: err, Duration: time.Since(start)}
	}

	content := genai.NewContentFromText(tc.Input, "user")

	// Collect all response text
	var responseText strings.Builder
	var opts []runner.RunOption
	if tc.InitialState != nil {
		opts = append(opts, runner.WithStateDelta(tc.InitialState))
	}

	for event, err := range r.Run(ctx, "eval-user", sessionID, content, agent.RunConfig{}, opts...) {
		if err != nil {
			return Result{
				Name:     tc.Name,
				Passed:   false,
				Error:    fmt.Errorf("agent run error: %w", err),
				Duration: time.Since(start),
			}
		}
		if event != nil && event.Content != nil {
			for _, p := range event.Content.Parts {
				if p.Text != "" {
					responseText.WriteString(p.Text)
				}
			}
		}
	}

	response := responseText.String()

	// Run assertion
	if tc.Assert != nil {
		if err := tc.Assert(response); err != nil {
			return Result{
				Name:     tc.Name,
				Passed:   false,
				Response: response,
				Error:    err,
				Duration: time.Since(start),
			}
		}
	}

	return Result{
		Name:     tc.Name,
		Passed:   true,
		Response: response,
		Duration: time.Since(start),
	}
}

// --- Built-in Assertion Functions ---

// Contains asserts the response contains the given substring (case-insensitive).
func Contains(substr string) AssertFunc {
	return func(response string) error {
		if !strings.Contains(strings.ToLower(response), strings.ToLower(substr)) {
			return fmt.Errorf("expected response to contain %q", substr)
		}
		return nil
	}
}

// Exact asserts the response exactly matches the given string (trimmed).
func Exact(expected string) AssertFunc {
	return func(response string) error {
		if strings.TrimSpace(response) != strings.TrimSpace(expected) {
			return fmt.Errorf("expected %q, got %q", expected, strings.TrimSpace(response))
		}
		return nil
	}
}

// MatchesRegex asserts the response matches the given regex pattern.
func MatchesRegex(pattern string) AssertFunc {
	re := regexp.MustCompile(pattern)
	return func(response string) error {
		if !re.MatchString(response) {
			return fmt.Errorf("expected response to match /%s/", pattern)
		}
		return nil
	}
}

// NotEmpty asserts the response is not empty.
func NotEmpty() AssertFunc {
	return func(response string) error {
		if strings.TrimSpace(response) == "" {
			return fmt.Errorf("expected non-empty response")
		}
		return nil
	}
}

// OneOf asserts the response is exactly one of the given options.
func OneOf(options ...string) AssertFunc {
	return func(response string) error {
		trimmed := strings.TrimSpace(response)
		for _, opt := range options {
			if trimmed == opt {
				return nil
			}
		}
		return fmt.Errorf("expected one of %v, got %q", options, trimmed)
	}
}

// All combines multiple assertions. All must pass.
func All(fns ...AssertFunc) AssertFunc {
	return func(response string) error {
		for _, fn := range fns {
			if err := fn(response); err != nil {
				return err
			}
		}
		return nil
	}
}

// Custom allows a user-defined assertion function.
func Custom(name string, fn func(string) bool) AssertFunc {
	return func(response string) error {
		if !fn(response) {
			return fmt.Errorf("custom assertion %q failed", name)
		}
		return nil
	}
}
