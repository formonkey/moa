package skillopt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EditType is the kind of modification to apply to the skill document.
type EditType string

const (
	// EditAdd inserts new content into the skill document.
	EditAdd EditType = "add"
	// EditReplace substitutes existing content with new content.
	EditReplace EditType = "replace"
	// EditDelete removes content from the skill document.
	EditDelete EditType = "delete"
)

// Edit is a single proposed modification to the skill document.
type Edit struct {
	// Type of edit operation.
	Type EditType `json:"type"`
	// Section is a label for the part of the skill being modified (for logging).
	Section string `json:"section"`
	// Target is the exact text to find for replace/delete operations.
	// Ignored for "add" operations.
	Target string `json:"target,omitempty"`
	// Content is the new text to insert (for add) or substitute (for replace).
	// Ignored for "delete" operations.
	Content string `json:"content,omitempty"`
	// Rationale explains why this edit was proposed (from the optimizer LLM).
	Rationale string `json:"rationale"`
}

// String returns a human-readable summary of the edit.
func (e Edit) String() string {
	switch e.Type {
	case EditAdd:
		return fmt.Sprintf("[ADD] %s: %s (%s)", e.Section, truncate(e.Content, 80), e.Rationale)
	case EditReplace:
		return fmt.Sprintf("[REPLACE] %s: %s → %s (%s)",
			e.Section, truncate(e.Target, 40), truncate(e.Content, 40), e.Rationale)
	case EditDelete:
		return fmt.Sprintf("[DELETE] %s: %s (%s)", e.Section, truncate(e.Target, 80), e.Rationale)
	default:
		return fmt.Sprintf("[%s] %s", e.Type, e.Rationale)
	}
}

// ApplyEdits applies a list of edits to the skill document, returning the
// modified document. Edits are applied sequentially.
//
// For "add": content is appended at the end of the document (or after section marker).
// For "replace": the first occurrence of Target is replaced with Content.
// For "delete": the first occurrence of Target is removed.
//
// If a replace/delete target is not found, that edit is skipped and included
// in the returned skipped list.
func ApplyEdits(skill string, edits []Edit) (result string, applied []Edit, skipped []Edit) {
	result = skill

	for _, edit := range edits {
		switch edit.Type {
		case EditAdd:
			// Append to end with a blank line separator
			if !strings.HasSuffix(result, "\n") {
				result += "\n"
			}
			result += "\n" + edit.Content
			applied = append(applied, edit)

		case EditReplace:
			if edit.Target == "" {
				skipped = append(skipped, edit)
				continue
			}
			if strings.Contains(result, edit.Target) {
				result = strings.Replace(result, edit.Target, edit.Content, 1)
				applied = append(applied, edit)
			} else {
				skipped = append(skipped, edit)
			}

		case EditDelete:
			if edit.Target == "" {
				skipped = append(skipped, edit)
				continue
			}
			if strings.Contains(result, edit.Target) {
				result = strings.Replace(result, edit.Target, "", 1)
				applied = append(applied, edit)
			} else {
				skipped = append(skipped, edit)
			}

		default:
			skipped = append(skipped, edit)
		}
	}

	// Clean up excessive blank lines
	for strings.Contains(result, "\n\n\n") {
		result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
	}

	return result, applied, skipped
}

// ParseEditsJSON parses a JSON array of edits from the optimizer LLM response.
// It is lenient: it tries to extract a JSON array from the text even if
// surrounded by markdown code fences or other text.
func ParseEditsJSON(text string) ([]Edit, error) {
	// Try to find JSON array in the text
	jsonStr := extractJSONArray(text)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON array found in optimizer response")
	}

	var edits []Edit
	if err := json.Unmarshal([]byte(jsonStr), &edits); err != nil {
		return nil, fmt.Errorf("parsing edits JSON: %w", err)
	}

	// Validate
	for i, e := range edits {
		if e.Type == "" {
			return nil, fmt.Errorf("edit[%d]: missing type", i)
		}
		switch e.Type {
		case EditAdd:
			if e.Content == "" {
				return nil, fmt.Errorf("edit[%d]: add requires content", i)
			}
		case EditReplace:
			if e.Target == "" || e.Content == "" {
				return nil, fmt.Errorf("edit[%d]: replace requires target and content", i)
			}
		case EditDelete:
			if e.Target == "" {
				return nil, fmt.Errorf("edit[%d]: delete requires target", i)
			}
		default:
			return nil, fmt.Errorf("edit[%d]: unknown type %q", i, e.Type)
		}
	}

	return edits, nil
}

// extractJSONArray tries to find a JSON array in the given text,
// handling markdown code fences and leading/trailing text.
func extractJSONArray(text string) string {
	// Remove markdown code fences
	text = strings.ReplaceAll(text, "```json", "")
	text = strings.ReplaceAll(text, "```", "")

	// Find the first [ and last ]
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start == -1 || end == -1 || end <= start {
		return ""
	}

	return text[start : end+1]
}
