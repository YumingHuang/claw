package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/YumingHuang/claw/internal/audit"
	"github.com/YumingHuang/claw/internal/models"
)

// RunCommandTool executes whitelisted system commands.
type RunCommandTool struct {
	allowed           map[string]bool
	deniedArgPatterns []*regexp.Regexp
	maxOutputChars    int
	timeout           time.Duration
	auditor           *audit.Logger
}

func NewRunCommandTool(allowedCommands []string, deniedArgPatterns []string, maxOutputChars int, timeout time.Duration, auditor *audit.Logger) *RunCommandTool {
	allowed := make(map[string]bool, len(allowedCommands))
	for _, c := range allowedCommands {
		allowed[c] = true
	}
	patterns := make([]*regexp.Regexp, 0, len(deniedArgPatterns))
	for _, p := range deniedArgPatterns {
		if re, err := regexp.Compile(p); err == nil {
			patterns = append(patterns, re)
		}
	}
	// Built-in safety patterns: block metadata endpoints and sensitive paths.
	if len(patterns) == 0 {
		patterns = append(patterns,
			regexp.MustCompile(`169\.254\.169\.254`),
			regexp.MustCompile(`(?i)metadata\.google`),
			regexp.MustCompile(`(?i)/etc/(passwd|shadow)`),
		)
	}
	return &RunCommandTool{
		allowed:           allowed,
		deniedArgPatterns: patterns,
		maxOutputChars:    maxOutputChars,
		timeout:           timeout,
		auditor:           auditor,
	}
}

func (t *RunCommandTool) Name() string        { return "run_command" }
func (t *RunCommandTool) Description() string { return "Run a system command" }
func (t *RunCommandTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Command to run"},"args":{"type":"array","items":{"type":"string"},"description":"Command arguments"}},"required":["command"]}`)
}

func (t *RunCommandTool) Execute(ctx context.Context, params json.RawMessage) (models.ToolResult, error) {
	var p struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return models.ToolResult{Content: fmt.Sprintf("invalid parameters: %v", err), IsError: true}, nil
	}

	if !t.allowed[p.Command] {
		return models.ToolResult{Content: fmt.Sprintf("command not allowed: %s", p.Command), IsError: true}, nil
	}

	// Check args against denied patterns.
	fullArgs := strings.Join(p.Args, " ")
	for _, re := range t.deniedArgPatterns {
		if re.MatchString(fullArgs) {
			return models.ToolResult{Content: fmt.Sprintf("argument blocked by security policy: %s", re.String()), IsError: true}, nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.Command, p.Args...)
	output, err := cmd.CombinedOutput()

	content := string(output)
	if len(content) > t.maxOutputChars {
		content = content[:t.maxOutputChars] + "\n... [truncated]"
	}

	if err != nil {
		result := models.ToolResult{Content: fmt.Sprintf("%s\nerror: %v", content, err), IsError: true}
		if t.auditor != nil {
			t.auditor.LogCommandRun(ctx, p.Command, p.Args, result)
		}
		return result, nil
	}

	result := models.ToolResult{Content: content}
	if t.auditor != nil {
		t.auditor.LogCommandRun(ctx, p.Command, p.Args, result)
	}
	return result, nil
}
