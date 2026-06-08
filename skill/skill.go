// Package skill provides a specialized capability bundle for go-brain agents.
//
// A Skill injects specific system instructions and a curated set of tools
// into an Agent to narrow its focus. Skills implement the tool.Toolset interface,
// making them directly composable with LLM agents.
//
// This is a go-brain exclusive feature not found in ADK Go.
package skill

import "github.com/formonkey/moa/tool"

// Skill represents a specialized capability bundle.
// Inspired by Goclaw and ADK, a skill injects specific system instructions
// and a curated set of tools into an Agent to narrow its focus.
//
// Skill also implements tool.Toolset, so it can be directly passed to
// llmagent.Config.Toolsets.
type Skill interface {
	tool.Toolset

	Description() string

	// SystemDirective provides context to the LLM on how to use the skill's tools.
	SystemDirective() string
}

// Registry manages available skills in the library.
type Registry struct {
	skills map[string]Skill
}

func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]Skill),
	}
}

func (r *Registry) Register(s Skill) {
	r.skills[s.Name()] = s
}

func (r *Registry) Get(name string) (Skill, bool) {
	s, ok := r.skills[name]
	return s, ok
}

func (r *Registry) All() []Skill {
	result := make([]Skill, 0, len(r.skills))
	for _, s := range r.skills {
		result = append(result, s)
	}
	return result
}

// --- Simple Skill implementation ---

// SimpleSkill is a basic Skill implementation for quick prototyping.
type SimpleSkill struct {
	SkillName        string
	SkillDescription string
	Directive        string
	SkillTools       []tool.Tool
}

func (s *SimpleSkill) Name() string           { return s.SkillName }
func (s *SimpleSkill) Description() string    { return s.SkillDescription }
func (s *SimpleSkill) SystemDirective() string { return s.Directive }
func (s *SimpleSkill) Tools() []tool.Tool      { return s.SkillTools }

// SetDirective updates the skill's system directive dynamically.
// This is used by the skill optimizer during training.
func (s *SimpleSkill) SetDirective(d string) { s.Directive = d }

// MarshalMarkdown serializes the skill as a markdown document suitable
// for deployment as a best_skill.md artifact.
func (s *SimpleSkill) MarshalMarkdown() string {
	md := "# " + s.SkillName + "\n\n"
	if s.SkillDescription != "" {
		md += "> " + s.SkillDescription + "\n\n"
	}
	md += "## System Directive\n\n"
	md += s.Directive + "\n"
	return md
}

// UnmarshalMarkdown loads a SimpleSkill from a markdown document.
// It expects the format produced by MarshalMarkdown:
//
//	# SkillName
//	> Description
//	## System Directive
//	<directive content>
func UnmarshalMarkdown(md string) (*SimpleSkill, error) {
	lines := splitLines(md)
	skill := &SimpleSkill{}

	var section string
	var directiveLines []string

	for _, line := range lines {
		trimmed := trimString(line)

		if hasPrefix(trimmed, "# ") && !hasPrefix(trimmed, "## ") {
			skill.SkillName = trimmed[2:]
			continue
		}
		if hasPrefix(trimmed, "> ") {
			skill.SkillDescription = trimmed[2:]
			continue
		}
		if hasPrefix(trimmed, "## System Directive") {
			section = "directive"
			continue
		}
		if section == "directive" {
			directiveLines = append(directiveLines, line)
		}
	}

	skill.Directive = joinLines(directiveLines)
	return skill, nil
}

// String helpers to avoid importing strings in this file.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimString(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	// Trim leading/trailing empty lines
	start, end := 0, len(lines)
	for start < end && trimString(lines[start]) == "" {
		start++
	}
	for end > start && trimString(lines[end-1]) == "" {
		end--
	}
	result := ""
	for i := start; i < end; i++ {
		if i > start {
			result += "\n"
		}
		result += lines[i]
	}
	return result
}

var _ Skill = (*SimpleSkill)(nil)
