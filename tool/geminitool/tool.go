// Package geminitool provides a generic wrapper for any Gemini native tool.
//
// Use this to wrap any genai.Tool (Retrieval, CodeExecution, etc.) as a
// go-brain NativeTool that can be passed to an LLM agent.
//
// Usage:
//
//	// Google Search
//	searchTool := geminitool.New("google_search", "Search the web", &genai.Tool{
//	    GoogleSearch: &genai.GoogleSearch{},
//	})
//
//	// Code Execution
//	codeTool := geminitool.New("code_execution", "Execute Python code", &genai.Tool{
//	    CodeExecution: &genai.ToolCodeExecution{},
//	})
//
//	// Retrieval
//	retrievalTool := geminitool.New("data_retrieval", "Retrieve documents", &genai.Tool{
//	    Retrieval: &genai.Retrieval{...},
//	})
package geminitool

import (
	"fmt"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool"
)

type geminiTool struct {
	name        string
	description string
	value       *genai.Tool
}

// New creates a new Gemini native tool wrapper.
func New(name, description string, t *genai.Tool) tool.NativeTool {
	return &geminiTool{
		name:        name,
		description: description,
		value:       t,
	}
}

// GoogleSearch creates a pre-configured Google Search tool.
func GoogleSearch() tool.NativeTool {
	return New("google_search",
		"Performs a Google search to retrieve information from the web.",
		&genai.Tool{GoogleSearch: &genai.GoogleSearch{}})
}

// CodeExecution creates a pre-configured Code Execution tool.
func CodeExecution() tool.NativeTool {
	return New("code_execution",
		"Executes Python code in a sandboxed environment.",
		&genai.Tool{CodeExecution: &genai.ToolCodeExecution{}})
}

func (t *geminiTool) Name() string        { return t.name }
func (t *geminiTool) Description() string { return t.description }
func (t *geminiTool) IsNative() bool      { return true }
func (t *geminiTool) IsLongRunning() bool { return false }

// ProcessRequest adds the Gemini native tool to the LLM request config.
func (t *geminiTool) ProcessRequest(req *model.LLMRequest) error {
	if req == nil {
		return fmt.Errorf("geminitool: llm request is nil")
	}
	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	req.Config.Tools = append(req.Config.Tools, t.value)
	return nil
}

var _ tool.NativeTool = (*geminiTool)(nil)
