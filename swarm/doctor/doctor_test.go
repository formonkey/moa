package doctor_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/formonkey/moa/swarm/doctor"
)

func TestDoctorCheckValidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "swarm.yaml")
	yaml := `name: test-swarm
agents:
  - name: coder
    type: llm
    model: gemini
    system: "You are a coder"
`
	os.WriteFile(yamlPath, []byte(yaml), 0644)

	report := doctor.Check(context.Background(), yamlPath)
	var buf bytes.Buffer
	report.Print(&buf)
	if buf.Len() == 0 {
		t.Fatal("expected non-empty print output")
	}
}

func TestDoctorCheckMissingFile(t *testing.T) {
	report := doctor.Check(context.Background(), "/nonexistent/swarm.yaml")
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if report.OK() {
		t.Fatal("expected not OK for missing file")
	}
}
