package functiontool_test

import (
	"context"
	"testing"

	"github.com/formonkey/moa/tool/functiontool"
)

type AddArgs struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
}

type AddResult struct {
	Sum float64 `json:"sum"`
}

func TestFunctionToolFromFunc(t *testing.T) {
	ft, err := functiontool.New[AddArgs, AddResult](
		functiontool.Config{
			Name:        "add",
			Description: "Adds two numbers",
		},
		func(ctx context.Context, args AddArgs) (AddResult, error) {
			return AddResult{Sum: args.A + args.B}, nil
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ft.Name() != "add" {
		t.Fatal("wrong name")
	}
	if ft.IsNative() {
		t.Fatal("should not be native")
	}
	if ft.Declaration() == nil {
		t.Fatal("expected declaration")
	}

	result, err := ft.Execute(context.Background(), map[string]any{"a": 3.0, "b": 4.0})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	sum, ok := result["sum"]
	if !ok {
		t.Fatalf("expected sum in result, got: %v", result)
	}
	if sum != 7.0 {
		t.Fatalf("expected 7, got %v", sum)
	}
}

type GreetArgs struct {
	Name string `json:"name"`
}

type GreetResult struct {
	Message string `json:"message"`
}

func TestFunctionToolString(t *testing.T) {
	ft, _ := functiontool.New[GreetArgs, GreetResult](
		functiontool.Config{
			Name:        "greet",
			Description: "Greets a person",
		},
		func(ctx context.Context, args GreetArgs) (GreetResult, error) {
			return GreetResult{Message: "Hello, " + args.Name}, nil
		},
	)

	result, err := ft.Execute(context.Background(), map[string]any{"name": "Alice"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result["message"] != "Hello, Alice" {
		t.Fatalf("expected 'Hello, Alice', got %v", result["message"])
	}
}

func TestFunctionToolLongRunning(t *testing.T) {
	ft, _ := functiontool.New[AddArgs, AddResult](
		functiontool.Config{
			Name:          "slow_add",
			Description:   "Slow adder",
			IsLongRunning: true,
		},
		func(ctx context.Context, args AddArgs) (AddResult, error) {
			return AddResult{Sum: args.A + args.B}, nil
		},
	)
	if !ft.IsLongRunning() {
		t.Fatal("expected long running")
	}
}

type IntBoolArgs struct {
	Count   int    `json:"count"`
	Flag    bool   `json:"flag"`
	Tags    []string `json:"tags"`
}

type IntBoolResult struct {
	Status string `json:"status"`
}

func TestFunctionToolIntBoolSlice(t *testing.T) {
	ft, err := functiontool.New[IntBoolArgs, IntBoolResult](
		functiontool.Config{
			Name:        "process",
			Description: "Process data",
		},
		func(ctx context.Context, args IntBoolArgs) (IntBoolResult, error) {
			return IntBoolResult{Status: "ok"}, nil
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	decl := ft.Declaration()
	if decl == nil {
		t.Fatal("nil declaration")
	}
	if decl.Parameters == nil {
		t.Fatal("nil parameters")
	}
	if decl.Parameters.Properties["count"] == nil {
		t.Fatal("missing count property")
	}
	if decl.Parameters.Properties["flag"] == nil {
		t.Fatal("missing flag property")
	}
	if decl.Parameters.Properties["tags"] == nil {
		t.Fatal("missing tags property")
	}

	result, err := ft.Execute(context.Background(), map[string]any{
		"count": float64(5),
		"flag":  true,
		"tags":  []any{"a", "b"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result["status"] != "ok" {
		t.Fatal("wrong status")
	}
}

func TestFunctionToolDescription(t *testing.T) {
	ft, _ := functiontool.New[AddArgs, AddResult](
		functiontool.Config{Name: "desc_test", Description: "Test desc"},
		func(ctx context.Context, args AddArgs) (AddResult, error) {
			return AddResult{}, nil
		},
	)
	if ft.Description() != "Test desc" {
		t.Fatal("wrong description")
	}
}

