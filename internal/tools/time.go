package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/YumingHuang/claw/internal/models"
)

type TimeTool struct{}

func NewTimeTool() *TimeTool { return &TimeTool{} }

func (t *TimeTool) Name() string        { return "get_current_time" }
func (t *TimeTool) Description() string { return "Returns the current date and time" }
func (t *TimeTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string","description":"IANA timezone, e.g. Asia/Shanghai"}},"required":[]}`)
}

func (t *TimeTool) Execute(_ context.Context, params json.RawMessage) (models.ToolResult, error) {
	var p struct {
		Timezone string `json:"timezone"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}

	loc := time.Local
	if p.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(p.Timezone)
		if err != nil {
			return models.ToolResult{Content: fmt.Sprintf("invalid timezone: %s", p.Timezone), IsError: true}, nil
		}
	}

	return models.ToolResult{Content: time.Now().In(loc).Format(time.RFC3339)}, nil
}
