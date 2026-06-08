package native

import (
	"github.com/formonkey/moa/model"
	"google.golang.org/genai"
)

// GoogleSearch is a native Gemini tool. It modifies the LLMRequest directly 
// to instruct the Google GenAI backend to use Google Search grounding.
type GoogleSearch struct{}

func (s *GoogleSearch) Name() string {
	return "google_search"
}

func (s *GoogleSearch) Description() string {
	return "Performs a Google search to retrieve information from the web. Handled natively by Gemini."
}

func (s *GoogleSearch) IsNative() bool      { return true }
func (s *GoogleSearch) IsLongRunning() bool { return false }

// ProcessRequest implements the tool.NativeTool interface.
func (s *GoogleSearch) ProcessRequest(req *model.LLMRequest) error {
	req.Tools = append(req.Tools, &genai.Tool{
		GoogleSearch: &genai.GoogleSearch{},
	})
	return nil
}
