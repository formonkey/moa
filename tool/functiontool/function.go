// Package functiontool provides a way to create tools from Go functions using generics.
//
// It automatically infers JSON Schema from Go types for both input and output,
// handles HITL confirmation, and supports long-running operations.
//
// Usage:
//
//	type MyArgs struct {
//	    Query string `json:"query" jsonschema:"description=The search query"`
//	}
//	type MyResult struct {
//	    Answer string `json:"answer"`
//	}
//
//	tool, err := functiontool.New(functiontool.Config{
//	    Name:        "search",
//	    Description: "Searches for information",
//	}, func(ctx tool.Context, args MyArgs) (MyResult, error) {
//	    return MyResult{Answer: "42"}, nil
//	})
package functiontool

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/formonkey/moa/tool"
	"google.golang.org/genai"
)

// Func represents a Go function that can be wrapped in a tool.
type Func[TArgs, TResults any] func(ctx context.Context, args TArgs) (TResults, error)

// Config is the input for creating a new FunctionTool.
type Config struct {
	Name        string
	Description string

	// IsLongRunning marks the tool as a long-running operation.
	IsLongRunning bool

	// RequireConfirmation forces HITL confirmation before execution.
	RequireConfirmation bool
}

// New creates a new Tool from a Go function using generics.
// Input/output schemas are inferred automatically from TArgs and TResults types.
func New[TArgs, TResults any](cfg Config, handler Func[TArgs, TResults]) (tool.RunnableTool, error) {
	var zeroArgs TArgs
	argsType := reflect.TypeOf(zeroArgs)
	for argsType != nil && argsType.Kind() == reflect.Pointer {
		argsType = argsType.Elem()
	}
	if argsType == nil || (argsType.Kind() != reflect.Struct && argsType.Kind() != reflect.Map) {
		return nil, fmt.Errorf("functiontool: input type must be a struct or map, got: %v", argsType)
	}

	inputSchema := inferSchema(argsType)

	return &functionTool[TArgs, TResults]{
		cfg:           cfg,
		handler:       handler,
		inputSchema:   inputSchema,
	}, nil
}

// --- functionTool implementation ---

type functionTool[TArgs, TResults any] struct {
	cfg         Config
	handler     Func[TArgs, TResults]
	inputSchema *genai.Schema
}

func (f *functionTool[TArgs, TResults]) Name() string        { return f.cfg.Name }
func (f *functionTool[TArgs, TResults]) Description() string { return f.cfg.Description }
func (f *functionTool[TArgs, TResults]) IsNative() bool      { return false }
func (f *functionTool[TArgs, TResults]) IsLongRunning() bool { return f.cfg.IsLongRunning }

func (f *functionTool[TArgs, TResults]) Declaration() *genai.FunctionDeclaration {
	decl := &genai.FunctionDeclaration{
		Name:        f.Name(),
		Description: f.Description(),
		Parameters:  f.inputSchema,
	}

	if f.cfg.IsLongRunning {
		note := "\n\nNOTE: This is a long-running operation. Do not call this tool again if it has already returned some intermediate or pending status."
		decl.Description += note
	}

	return decl
}

func (f *functionTool[TArgs, TResults]) Execute(ctx context.Context, args map[string]any) (result map[string]any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in tool %q: %v\nstack: %s", f.Name(), r, debug.Stack())
		}
	}()

	// HITL check
	if f.cfg.RequireConfirmation {
		if cCtx, ok := ctx.(tool.ConfirmationContext); ok {
			status := cCtx.GetConfirmation()
			if status == nil || (!status.Confirmed && !status.Rejected) {
				_ = cCtx.RequestConfirmation(
					fmt.Sprintf("Please approve tool call: %s()", f.Name()), args)
				return nil, tool.ErrConfirmationRequired
			}
			if status.Rejected {
				return nil, tool.ErrConfirmationRejected
			}
		}
	}

	// Convert map[string]any → TArgs via JSON round-trip
	input, err := convertTo[TArgs](args)
	if err != nil {
		return nil, fmt.Errorf("functiontool: failed to convert args for %q: %w", f.Name(), err)
	}

	// Execute the handler
	output, err := f.handler(ctx, input)
	if err != nil {
		return nil, err
	}

	// Convert TResults → map[string]any via JSON round-trip
	resp, err := convertFrom(output)
	if err != nil {
		// If result can't be converted to map, wrap it
		return map[string]any{"result": output}, nil
	}
	return resp, nil
}

// --- Schema inference ---

// inferSchema generates a genai.Schema from a Go reflect.Type.
// Supports struct tags: `json:"name"` for field names, `jsonschema:"description=..."` for docs.
func inferSchema(t reflect.Type) *genai.Schema {
	if t.Kind() == reflect.Map {
		return &genai.Schema{
			Type: genai.TypeObject,
		}
	}

	props := make(map[string]*genai.Schema)
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		name := field.Name
		if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] != "" && parts[0] != "-" {
				name = parts[0]
			}
			// Skip fields with json:"-"
			if parts[0] == "-" {
				continue
			}
		}

		fieldSchema := typeToSchema(field.Type)

		// Parse jsonschema tag for description
		if jsTag := field.Tag.Get("jsonschema"); jsTag != "" {
			for _, part := range strings.Split(jsTag, ",") {
				kv := strings.SplitN(part, "=", 2)
				if len(kv) == 2 && kv[0] == "description" {
					fieldSchema.Description = kv[1]
				}
				if kv[0] == "required" {
					required = append(required, name)
				}
			}
		}

		props[name] = fieldSchema
	}

	return &genai.Schema{
		Type:       genai.TypeObject,
		Properties: props,
		Required:   required,
	}
}

func typeToSchema(t reflect.Type) *genai.Schema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return &genai.Schema{Type: genai.TypeString}
	case reflect.Bool:
		return &genai.Schema{Type: genai.TypeBoolean}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &genai.Schema{Type: genai.TypeInteger}
	case reflect.Float32, reflect.Float64:
		return &genai.Schema{Type: genai.TypeNumber}
	case reflect.Slice, reflect.Array:
		return &genai.Schema{
			Type:  genai.TypeArray,
			Items: typeToSchema(t.Elem()),
		}
	case reflect.Struct:
		return inferSchema(t)
	case reflect.Map:
		return &genai.Schema{Type: genai.TypeObject}
	default:
		return &genai.Schema{Type: genai.TypeString}
	}
}

// --- JSON conversion helpers ---

func convertTo[T any](m map[string]any) (T, error) {
	var result T
	data, err := json.Marshal(m)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

func convertFrom(v any) (map[string]any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(data, &result)
	return result, err
}
