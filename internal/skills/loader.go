package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YumingHuang/claw/internal/tools"
	"gopkg.in/yaml.v3"
)

type skillManifest struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	Enabled     bool     `yaml:"enabled"`
}

// Load scans a skills directory, validates enabled skills, and returns their
// instructions concatenated as a system prompt supplement.
func Load(dir string, registry *tools.Registry) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read skills dir: %w", err)
	}

	var sections []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(dir, entry.Name())
		manifest, instructions, err := loadSkill(skillDir, registry)
		if err != nil {
			return "", err
		}
		if manifest == nil || !manifest.Enabled {
			continue
		}

		section := fmt.Sprintf("[Skill: %s]\n%s", manifest.Name, strings.TrimSpace(instructions))
		sections = append(sections, section)
	}

	return strings.Join(sections, "\n\n"), nil
}

func loadSkill(dir string, registry *tools.Registry) (*skillManifest, string, error) {
	manifestPath := filepath.Join(dir, "skill.yaml")
	instructionsPath := filepath.Join(dir, "instructions.md")

	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", manifestPath, err)
	}

	var manifest skillManifest
	if err := yaml.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	if manifest.Name == "" {
		manifest.Name = filepath.Base(dir)
	}

	for _, toolName := range manifest.Tools {
		if _, ok := registry.Get(toolName); !ok {
			return nil, "", fmt.Errorf("skill %q references unknown tool %q", manifest.Name, toolName)
		}
	}

	instructionsBytes, err := os.ReadFile(instructionsPath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", instructionsPath, err)
	}

	return &manifest, string(instructionsBytes), nil
}
