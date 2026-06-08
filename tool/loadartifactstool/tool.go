// Package loadartifactstool provides a tool that allows the LLM to load artifacts
// on demand. It injects artifact availability info into system instructions and
// loads artifact content when the model calls load_artifacts().
package loadartifactstool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/formonkey/moa/model"
	"github.com/formonkey/moa/tool"
)

const artifactInstructions = `You have a list of artifacts:
  %s

When the user asks questions about any of the artifacts, you should call the ` + "`load_artifacts`" + ` function to load the artifact. Do not generate any text other than the function call. Whenever you are asked about artifacts, you should first load it. You must always load an artifact to access its content, even if it has been loaded before.`

// ArtifactLoader defines the minimal interface needed to list and load artifacts.
type ArtifactLoader interface {
	ListArtifacts(ctx context.Context) ([]string, error)
	LoadArtifact(ctx context.Context, name string) (*genai.Part, error)
}

type artifactsTool struct {
	loader ArtifactLoader
}

// New creates a new load_artifacts tool.
func New(loader ArtifactLoader) tool.Tool {
	return &artifactsTool{loader: loader}
}

func (t *artifactsTool) Name() string        { return "load_artifacts" }
func (t *artifactsTool) Description() string { return "Loads artifacts and adds them to the session." }
func (t *artifactsTool) IsNative() bool      { return false }
func (t *artifactsTool) IsLongRunning() bool { return false }

func (t *artifactsTool) Declaration() *genai.FunctionDeclaration {
	return &genai.FunctionDeclaration{
		Name:        "load_artifacts",
		Description: "Loads the specified artifacts by name.",
		Parameters: &genai.Schema{
			Type: "OBJECT",
			Properties: map[string]*genai.Schema{
				"artifact_names": {
					Type:        "ARRAY",
					Description: "List of artifact filenames to load",
					Items:       &genai.Schema{Type: "STRING"},
				},
			},
		},
	}
}

func (t *artifactsTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	namesRaw, ok := args["artifact_names"]
	if !ok {
		return map[string]any{"artifact_names": []string{}}, nil
	}

	namesJSON, err := json.Marshal(namesRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal artifact_names: %w", err)
	}
	var names []string
	if err := json.Unmarshal(namesJSON, &names); err != nil {
		return nil, fmt.Errorf("failed to unmarshal artifact_names: %w", err)
	}

	return map[string]any{"artifact_names": names}, nil
}

// ProcessRequest injects artifact list into the system instruction and processes
// any load_artifacts function call responses by loading the actual artifact content.
func (t *artifactsTool) ProcessRequest(ctx context.Context, req *model.LLMRequest) error {
	// 1. List available artifacts and inject into system instructions
	if t.loader != nil {
		names, err := t.loader.ListArtifacts(ctx)
		if err != nil {
			return fmt.Errorf("failed to list artifacts: %w", err)
		}
		if len(names) > 0 {
			namesJSON, _ := json.Marshal(names)
			instruction := fmt.Sprintf(artifactInstructions, string(namesJSON))
			appendInstruction(req, instruction)
		}
	}

	// 2. Check if the last message is a load_artifacts function response
	if len(req.Contents) == 0 {
		return nil
	}
	last := req.Contents[len(req.Contents)-1]
	if last == nil || len(last.Parts) == 0 {
		return nil
	}
	firstPart := last.Parts[0]
	if firstPart.FunctionResponse == nil || firstPart.FunctionResponse.Name != "load_artifacts" {
		return nil
	}

	namesRaw, ok := firstPart.FunctionResponse.Response["artifact_names"]
	if !ok {
		return nil
	}
	namesJSON, _ := json.Marshal(namesRaw)
	var names []string
	_ = json.Unmarshal(namesJSON, &names)
	if len(names) == 0 {
		return nil
	}

	// 3. Load each artifact and append as content
	for _, name := range names {
		part, err := t.loader.LoadArtifact(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to load artifact %s: %w", name, err)
		}
		req.Contents = append(req.Contents, &genai.Content{
			Role: "user",
			Parts: []*genai.Part{
				{Text: "Artifact " + name + " is:"},
				part,
			},
		})
	}

	return nil
}

func appendInstruction(req *model.LLMRequest, text string) {
	if req.SystemInstruction == nil {
		req.SystemInstruction = &genai.Content{
			Role:  "system",
			Parts: []*genai.Part{{Text: text}},
		}
		return
	}
	existing := ""
	for _, p := range req.SystemInstruction.Parts {
		existing += p.Text
	}
	req.SystemInstruction = &genai.Content{
		Role:  "system",
		Parts: []*genai.Part{{Text: strings.TrimSpace(existing) + "\n\n" + text}},
	}
}

var _ tool.RunnableTool = (*artifactsTool)(nil)
