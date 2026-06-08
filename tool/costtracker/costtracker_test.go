package costtracker

import (
	"context"
	"strings"
	"testing"
)

func TestRecord(t *testing.T) {
	tracker := New(Config{
		PricePerInputToken:  0.00000015,
		PricePerOutputToken: 0.0000006,
	})

	tracker.Record("angular-dev", "research", "", 1000, 500)
	tracker.Record("angular-dev", "implement", "sub-1", 2000, 800)

	s := tracker.GetSummary()
	if s.TotalInputTokens != 3000 {
		t.Errorf("expected 3000 input, got %d", s.TotalInputTokens)
	}
	if s.TotalOutputTokens != 1300 {
		t.Errorf("expected 1300 output, got %d", s.TotalOutputTokens)
	}
	if s.Entries != 2 {
		t.Errorf("expected 2 entries, got %d", s.Entries)
	}
}

func TestCostCalculation(t *testing.T) {
	tracker := New(Config{
		PricePerInputToken:  0.001, // $1 per 1000 tokens
		PricePerOutputToken: 0.002,
	})

	tracker.Record("agent", "state", "", 1000, 500)
	s := tracker.GetSummary()

	expectedCost := 1000*0.001 + 500*0.002 // 1.0 + 1.0 = 2.0
	if s.TotalCost != expectedCost {
		t.Errorf("expected cost %.6f, got %.6f", expectedCost, s.TotalCost)
	}
}

func TestByAgent(t *testing.T) {
	tracker := New(Config{
		PricePerInputToken:  0.0001,
		PricePerOutputToken: 0.0002,
	})

	tracker.Record("agent-a", "s1", "", 100, 50)
	tracker.Record("agent-b", "s1", "", 200, 100)
	tracker.Record("agent-a", "s2", "", 300, 150)

	s := tracker.GetSummary()
	if len(s.ByAgent) != 2 {
		t.Errorf("expected 2 agents, got %d", len(s.ByAgent))
	}
	a := s.ByAgent["agent-a"]
	if a.InputTokens != 400 {
		t.Errorf("agent-a input: expected 400, got %d", a.InputTokens)
	}
}

func TestReset(t *testing.T) {
	tracker := New(Config{})
	tracker.Record("a", "s", "", 100, 50)
	tracker.Reset()

	s := tracker.GetSummary()
	if s.Entries != 0 {
		t.Errorf("expected 0 entries after reset, got %d", s.Entries)
	}
}

func TestDefaultCurrency(t *testing.T) {
	tracker := New(Config{})
	s := tracker.GetSummary()
	if s.Currency != "USD" {
		t.Errorf("expected USD, got %s", s.Currency)
	}
}

func TestCostSummaryTool(t *testing.T) {
	tracker := New(Config{
		PricePerInputToken:  0.00000015,
		PricePerOutputToken: 0.0000006,
	})
	tracker.Record("angular-dev", "research", "", 5000, 2000)

	tools, err := NewToolset(tracker)
	if err != nil {
		t.Fatal(err)
	}

	type runnableTool interface {
		Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	}

	var costTool runnableTool
	for _, tl := range tools {
		if tl.Name() == "cost_summary" {
			costTool = tl.(runnableTool)
		}
	}

	result, err := costTool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}

	totalCost, _ := result["total_cost"].(string)
	if !strings.HasPrefix(totalCost, "$") {
		t.Errorf("expected cost with $, got %v", totalCost)
	}
}

func TestRegister(t *testing.T) {
	tracker := New(Config{
		PricePerInputToken:  0.001,
		PricePerOutputToken: 0.002,
	})
	err := Register(tracker)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
}

func TestCustomCurrency(t *testing.T) {
	tracker := New(Config{Currency: "EUR"})
	s := tracker.GetSummary()
	if s.Currency != "EUR" {
		t.Errorf("expected EUR, got %s", s.Currency)
	}
}

func TestToolsetAccessors(t *testing.T) {
	tracker := New(Config{})
	tools, err := NewToolset(tracker)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least 1 tool")
	}
	tool := tools[0]
	if tool.Name() != "cost_summary" {
		t.Fatalf("expected 'cost_summary', got %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestEmptySummary(t *testing.T) {
	tracker := New(Config{})
	s := tracker.GetSummary()
	if s.TotalTokens != 0 {
		t.Errorf("expected 0 total tokens, got %d", s.TotalTokens)
	}
	if s.TotalCost != 0 {
		t.Errorf("expected 0 cost, got %.6f", s.TotalCost)
	}
	if len(s.ByAgent) != 0 {
		t.Errorf("expected empty by-agent, got %d", len(s.ByAgent))
	}
}
