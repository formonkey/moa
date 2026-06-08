package skill_test

import (
	"testing"

	"github.com/formonkey/moa/skill"
	"github.com/formonkey/moa/tool"
)

func TestSimpleSkill(t *testing.T) {
	s := &skill.SimpleSkill{
		SkillName:        "code-review",
		SkillDescription: "Reviews code",
		Directive:        "You are a code reviewer",
		SkillTools:       []tool.Tool{},
	}
	if s.Name() != "code-review" {
		t.Fatal("wrong name")
	}
	if s.Description() != "Reviews code" {
		t.Fatal("wrong description")
	}
	if s.SystemDirective() != "You are a code reviewer" {
		t.Fatal("wrong directive")
	}
	if len(s.Tools()) != 0 {
		t.Fatal("expected empty tools")
	}
}

func TestRegistry(t *testing.T) {
	reg := skill.NewRegistry()

	s1 := &skill.SimpleSkill{SkillName: "skill1", SkillDescription: "first"}
	s2 := &skill.SimpleSkill{SkillName: "skill2", SkillDescription: "second"}

	reg.Register(s1)
	reg.Register(s2)

	got, ok := reg.Get("skill1")
	if !ok || got.Name() != "skill1" {
		t.Fatal("expected to find skill1")
	}

	_, ok = reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

func TestSetDirective(t *testing.T) {
	s := &skill.SimpleSkill{
		SkillName: "test",
		Directive: "original",
	}
	if s.SystemDirective() != "original" {
		t.Fatalf("expected 'original', got %q", s.SystemDirective())
	}

	s.SetDirective("updated")
	if s.SystemDirective() != "updated" {
		t.Fatalf("expected 'updated', got %q", s.SystemDirective())
	}
}

func TestMarshalMarkdown(t *testing.T) {
	s := &skill.SimpleSkill{
		SkillName:        "code-review",
		SkillDescription: "Reviews code quality",
		Directive:        "You are an expert code reviewer.\nFocus on correctness.",
	}

	md := s.MarshalMarkdown()
	if md == "" {
		t.Fatal("expected non-empty markdown")
	}

	// Check that it contains the expected elements
	if !contains(md, "# code-review") {
		t.Fatal("missing skill name heading")
	}
	if !contains(md, "> Reviews code quality") {
		t.Fatal("missing description blockquote")
	}
	if !contains(md, "## System Directive") {
		t.Fatal("missing System Directive heading")
	}
	if !contains(md, "You are an expert code reviewer") {
		t.Fatal("missing directive content")
	}
}

func TestMarshalMarkdownNoDescription(t *testing.T) {
	s := &skill.SimpleSkill{
		SkillName: "simple",
		Directive: "Do stuff",
	}

	md := s.MarshalMarkdown()
	if contains(md, "> ") {
		t.Fatal("should not have description blockquote")
	}
}

func TestUnmarshalMarkdown(t *testing.T) {
	md := "# code-review\n\n> Reviews code quality\n\n## System Directive\n\nYou are an expert code reviewer.\nFocus on correctness.\n"

	s, err := skill.UnmarshalMarkdown(md)
	if err != nil {
		t.Fatalf("UnmarshalMarkdown: %v", err)
	}

	if s.SkillName != "code-review" {
		t.Fatalf("expected 'code-review', got %q", s.SkillName)
	}
	if s.SkillDescription != "Reviews code quality" {
		t.Fatalf("expected 'Reviews code quality', got %q", s.SkillDescription)
	}
	if s.Directive == "" {
		t.Fatal("expected non-empty directive")
	}
	if !contains(s.Directive, "You are an expert code reviewer") {
		t.Fatalf("directive should contain content, got %q", s.Directive)
	}
}

func TestUnmarshalMarkdownEmpty(t *testing.T) {
	s, err := skill.UnmarshalMarkdown("")
	if err != nil {
		t.Fatalf("UnmarshalMarkdown: %v", err)
	}
	if s.SkillName != "" {
		t.Fatalf("expected empty name, got %q", s.SkillName)
	}
}

func TestMarshalUnmarshalRoundtrip(t *testing.T) {
	original := &skill.SimpleSkill{
		SkillName:        "test-skill",
		SkillDescription: "A test skill",
		Directive:        "Line one\nLine two",
	}

	md := original.MarshalMarkdown()
	restored, err := skill.UnmarshalMarkdown(md)
	if err != nil {
		t.Fatalf("UnmarshalMarkdown: %v", err)
	}

	if restored.SkillName != original.SkillName {
		t.Fatalf("name mismatch: %q vs %q", restored.SkillName, original.SkillName)
	}
	if restored.SkillDescription != original.SkillDescription {
		t.Fatalf("description mismatch: %q vs %q", restored.SkillDescription, original.SkillDescription)
	}
	if !contains(restored.Directive, "Line one") || !contains(restored.Directive, "Line two") {
		t.Fatalf("directive content lost: %q", restored.Directive)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
