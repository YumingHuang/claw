// Package cron provides a scheduler that sends messages to the agent on a cron schedule.
package cron

import (
	"context"
	"log/slog"

	"github.com/robfig/cron/v3"

	"github.com/YumingHuang/claw/internal/config"
	"github.com/YumingHuang/claw/internal/gateway"
)

// Notifier sends a message to a target (e.g. feishu chat).
type Notifier interface {
	Notify(ctx context.Context, target, content string) error
}

// Scheduler runs cron jobs that send messages to the gateway.
type Scheduler struct {
	c  *cron.Cron
	gw *gateway.Gateway
}

// New creates a Scheduler from config. Call Start to begin.
func New(gw *gateway.Gateway, jobs []config.CronJobConfig, notifier Notifier) (*Scheduler, error) {
	c := cron.New()
	s := &Scheduler{c: c, gw: gw}

	for _, job := range jobs {
		j := job
		channel := j.Channel
		if channel == "" {
			channel = "cron"
		}
		sessionID := "cron:" + j.Name

		_, err := c.AddFunc(j.Schedule, func() {
			slog.Info("cron: executing job", "name", j.Name)
			resp, err := gw.HandleMessage(context.Background(), sessionID, channel, j.Message)
			if err != nil {
				slog.Error("cron: job failed", "name", j.Name, "error", err)
				return
			}
			slog.Info("cron: job completed", "name", j.Name, "response_length", len(resp.Message.Content))

			if j.Notify != "" && notifier != nil {
				if err := notifier.Notify(context.Background(), j.Notify, resp.Message.Content); err != nil {
					slog.Error("cron: notify failed", "name", j.Name, "target", j.Notify, "error", err)
				}
			}
		})
		if err != nil {
			return nil, err
		}
		slog.Info("cron: registered job", "name", j.Name, "schedule", j.Schedule)
	}

	return s, nil
}

// Start begins the cron scheduler.
func (s *Scheduler) Start() { s.c.Start() }

// Stop stops the cron scheduler and waits for running jobs to finish.
func (s *Scheduler) Stop() context.Context { return s.c.Stop() }
