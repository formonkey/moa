// Package instructionutil provides utilities for working with instruction templates.
//
// Templates support:
//   - {key} — resolved from session state
//   - {artifact.key} — resolved from artifact content
//   - {key?} — optional, no error if missing (replaced with "")
package instructionutil

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/formonkey/moa/session"
)

var placeholderRegex = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_.]*\??)\}`)

// ArtifactTextLoader can load artifact content as text.
type ArtifactTextLoader interface {
	LoadArtifactText(ctx context.Context, name string) (string, error)
}

// InjectSessionState replaces template placeholders with values from session state
// and optionally from artifacts.
//
// Placeholder formats:
//   - {key}           — replaced with state value; error if missing
//   - {key?}          — replaced with state value; empty string if missing (no error)
//   - {artifact.key}  — replaced with artifact text content; error if missing
//   - {artifact.key?} — replaced with artifact text content; empty string if missing
func InjectSessionState(ctx context.Context, template string, state session.ReadonlyState, artifacts ArtifactTextLoader) (string, error) {
	var errs []string

	result := placeholderRegex.ReplaceAllStringFunc(template, func(match string) string {
		// Extract key from {key} or {key?}
		inner := match[1 : len(match)-1]
		optional := false
		if strings.HasSuffix(inner, "?") {
			optional = true
			inner = inner[:len(inner)-1]
		}

		// Check for artifact prefix
		if strings.HasPrefix(inner, "artifact.") {
			artifactName := strings.TrimPrefix(inner, "artifact.")
			if artifacts == nil {
				if !optional {
					errs = append(errs, fmt.Sprintf("artifact loader not available for {artifact.%s}", artifactName))
				}
				return ""
			}
			text, err := artifacts.LoadArtifactText(ctx, artifactName)
			if err != nil {
				if !optional {
					errs = append(errs, fmt.Sprintf("failed to load artifact %q: %v", artifactName, err))
				}
				return ""
			}
			return text
		}

		// Regular state key
		val, err := state.Get(inner)
		if err != nil {
			if !optional {
				errs = append(errs, fmt.Sprintf("state key %q not found", inner))
			}
			return ""
		}
		return fmt.Sprint(val)
	})

	if len(errs) > 0 {
		return result, fmt.Errorf("template injection errors: %s", strings.Join(errs, "; "))
	}
	return result, nil
}
