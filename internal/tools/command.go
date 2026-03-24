package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/YumingHuang/claw/internal/audit"
	"github.com/YumingHuang/claw/internal/models"
)

// RunCommandTool executes whitelisted system commands.
type RunCommandTool struct {
	allowed        map[string]bool
	maxOutputChars int
	timeout        time.Duration
	auditor        *audit.Logger
}

func NewRunCommandTool(allowedCommands []string, maxOutputChars int, timeout time.Duration, auditor *audit.Logger) *RunCommandTool {
	allowed := make(map[string]bool, len(allowedCommands))
	for _, c := range allowedCommands {
		allowed[c] = true
	}
	return &RunCommandTool{allowed: allowed, maxOutputChars: maxOutputChars, timeout: timeout, auditor: auditor}
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
