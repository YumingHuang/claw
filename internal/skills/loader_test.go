package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YumingHuang/claw/internal/tools"
)

func TestLoad_Success(t *testing.T) {
	dir := t.TempDir()
	registry := tools.NewRegistry()
	if err := registry.Register(tools.NewTimeTool()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	writeSkill(t, dir, "time_helper", `name: "time_helper"
description: "Time helper"
tools: ["get_current_time"]
enabled: true
`, "Always prefer RFC3339 timestamps.")

	got, err := Load(dir, registry)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(got, "[Skill: time_helper]") {
		t.Fatalf("missing skill header: %q", got)
	}
	if !strings.Contains(got, "Always prefer RFC3339 timestamps.") {
		t.Fatalf("missing instructions: %q", got)
	}
}

func TestLoad_SkipsDisabledSkill(t *testing.T) {
	dir := t.TempDir()
	registry := tools.NewRegistry()
	if err := registry.Register(tools.NewTimeTool()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	writeSkill(t, dir, "disabled", `name: "disabled"
tools: ["get_current_time"]
enabled: false
`, "Do not include this.")

	got, err := Load(dir, registry)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty prompt supplement, got %q", got)
	}
}

func TestLoad_UnknownTool(t *testing.T) {
	dir := t.TempDir()
	registry := tools.NewRegistry()

	writeSkill(t, dir, "bad_skill", `name: "bad_skill"
tools: ["missing_tool"]
enabled: true
`, "Bad.")

	_, err := Load(dir, registry)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("err = %v", err)
	}
}

func writeSkill(t *testing.T, root, name, manifest, instructions string) {
	t.Helper()
	skillDir := filepath.Join(root, name)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "instructions.md"), []byte(instructions), 0644); err != nil {
		t.Fatalf("WriteFile instructions.md: %v", err)
	}
}
