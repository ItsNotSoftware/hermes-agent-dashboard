// Package derive turns raw inputs (crons, usage, system load, etc.) into
// the synthesized views the dashboard panels render: per-agent ops,
// mission timeline, and the mission-control command list.
package derive

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/diogo/hermes-agent-dashboard/internal/calendar"
	"github.com/diogo/hermes-agent-dashboard/internal/claude"
	"github.com/diogo/hermes-agent-dashboard/internal/hermes"
	"github.com/diogo/hermes-agent-dashboard/internal/loops"
	"github.com/diogo/hermes-agent-dashboard/internal/metrics"
	"github.com/diogo/hermes-agent-dashboard/internal/openai"
)

type NextJob struct {
	Name      string `json:"name"`
	NextRun   string `json:"next_run"`
	NextRunAt string `json:"next_run_at"`
}

type PausedJob struct {
	Name    string `json:"name"`
	NextRun string `json:"next_run"`
}

type FailedJob struct {
	Name       string `json:"name"`
	LastStatus string `json:"last_status"`
}

type AgentOps struct {
	Name        string      `json:"name"`
	State       string      `json:"state"` // ok | failed | paused | idle
	JobCount    int         `json:"job_count"`
	PausedCount int         `json:"paused_count"`
	FailedCount int         `json:"failed_count"`
	NextJob     *NextJob    `json:"next_job"`
	PausedJobs  []PausedJob `json:"paused_jobs"`
	FailedJobs  []FailedJob `json:"failed_jobs"`
}

type MissionEvent struct {
	TS       string `json:"ts"`
	Time     string `json:"time"`
	Label    string `json:"label"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Severity string `json:"severity"` // ok | warn | critical
}

type Command struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	State  string `json:"state"` // ok | warn | critical | idle
}

type MissionControl struct {
	Commands []Command `json:"commands"`
}

// Ops groups jobs by owner and summarizes health, mirroring get_agent_ops.
func Ops(sources []hermes.CronSource, crons []hermes.Cron) map[string]AgentOps {
	out := make(map[string]AgentOps, len(sources))
	for _, src := range sources {
		owner := src.Owner
		var jobs, paused, failed, upcoming []hermes.Cron
		for _, c := range crons {
			if c.Owner != owner && c.Profile != owner {
				continue
			}
			jobs = append(jobs, c)
			if c.State == "paused" {
				paused = append(paused, c)
			}
			if c.LastStatus != "" && c.LastStatus != "ok" {
				failed = append(failed, c)
			}
			if c.State != "paused" && c.NextRunAt != "" {
				upcoming = append(upcoming, c)
			}
		}
		sort.SliceStable(upcoming, func(i, j int) bool {
			return upcoming[i].NextRunAt < upcoming[j].NextRunAt
		})
		state := "ok"
		switch {
		case len(failed) > 0:
			state = "failed"
		case len(paused) > 0:
			state = "paused"
		case len(jobs) == 0:
			state = "idle"
		}
		ops := AgentOps{
			Name:        owner,
			State:       state,
			JobCount:    len(jobs),
			PausedCount: len(paused),
			FailedCount: len(failed),
		}
		if len(upcoming) > 0 {
			n := upcoming[0]
			ops.NextJob = &NextJob{Name: n.Name, NextRun: n.NextRun, NextRunAt: n.NextRunAt}
		}
		for _, p := range paused {
			ops.PausedJobs = append(ops.PausedJobs, PausedJob{Name: p.Name, NextRun: p.NextRun})
		}
		for _, f := range failed {
			ops.FailedJobs = append(ops.FailedJobs, FailedJob{Name: f.Name, LastStatus: f.LastStatus})
		}
		out[owner] = ops
	}
	return out
}

// Log assembles the mission timeline.
func Log(crons []hermes.Cron, plan *openai.Plan, cu *claude.Usage, mem, disk metrics.Mem, temp float64) []MissionEvent {
	now := time.Now().UTC().Format(time.RFC3339)
	nowLabel := "now"
	var items []MissionEvent

	for _, c := range crons {
		if c.LastRunAt == "" {
			continue
		}
		status := c.LastStatus
		if status == "" {
			status = "unknown"
		}
		sev := "ok"
		if status != "ok" {
			sev = "critical"
		}
		t := c.LastRun
		if t == "" {
			t = formatShort(c.LastRunAt)
		}
		owner := c.Owner
		if owner == "" {
			owner = c.Profile
		}
		if owner == "" {
			owner = "cron"
		}
		items = append(items, MissionEvent{
			TS: c.LastRunAt, Time: t, Label: owner,
			Title:  nonEmpty(c.Name, "Unnamed cron"),
			Detail: "last run " + status, Severity: sev,
		})
	}

	if temp >= 80 {
		items = append(items, MissionEvent{TS: now, Time: nowLabel, Label: "TEMP", Title: "CPU thermal critical", Detail: fmt.Sprintf("%.1fC", temp), Severity: "critical"})
	} else if temp >= 75 {
		items = append(items, MissionEvent{TS: now, Time: nowLabel, Label: "TEMP", Title: "CPU thermal warning", Detail: fmt.Sprintf("%.1fC", temp), Severity: "warn"})
	}

	pct := func(used, total float64) float64 {
		if total == 0 {
			return 0
		}
		return used / total * 100
	}
	ramPct := pct(mem.Used, mem.Total)
	if ramPct >= 85 {
		sev := "warn"
		if ramPct >= 95 {
			sev = "critical"
		}
		items = append(items, MissionEvent{TS: now, Time: nowLabel, Label: "RAM", Title: "Memory pressure", Detail: fmt.Sprintf("%.0f%% used", ramPct), Severity: sev})
	}
	diskPct := pct(disk.Used, disk.Total)
	if diskPct >= 85 {
		sev := "warn"
		if diskPct >= 95 {
			sev = "critical"
		}
		items = append(items, MissionEvent{TS: now, Time: nowLabel, Label: "DISK", Title: "Disk pressure", Detail: fmt.Sprintf("%.0f%% used", diskPct), Severity: sev})
	}

	usageEvent := func(provider, label string, used *float64) {
		if used == nil {
			return
		}
		if *used >= 80 {
			sev := "warn"
			if *used >= 90 {
				sev = "critical"
			}
			items = append(items, MissionEvent{
				TS: now, Time: nowLabel, Label: provider,
				Title:  label + " usage high",
				Detail: fmt.Sprintf("%.0f%% used", *used), Severity: sev,
			})
		}
	}

	if plan != nil {
		if plan.LimitReached {
			items = append(items, MissionEvent{TS: now, Time: nowLabel, Label: "GPT", Title: "OpenAI limit reached", Detail: "usage gate closed", Severity: "critical"})
		} else if !plan.Allowed {
			items = append(items, MissionEvent{TS: now, Time: nowLabel, Label: "GPT", Title: "OpenAI usage restricted", Detail: "not currently allowed", Severity: "warn"})
		}
		p := plan.PrimaryWindow.UsedPercent
		s := plan.SecondaryWindow.UsedPercent
		usageEvent("GPT", "5h", &p)
		usageEvent("GPT", "1w", &s)
	}
	if cu != nil && cu.UsageSource == "api" {
		if cu.FiveHourWindow != nil {
			usageEvent("Claude", "5h", cu.FiveHourWindow.UsedPercent)
		}
		if cu.OneWeekWindow != nil {
			usageEvent("Claude", "1w", cu.OneWeekWindow.UsedPercent)
		}
	}

	sort.SliceStable(items, func(i, j int) bool { return items[i].TS > items[j].TS })
	if len(items) > 8 {
		items = items[:8]
	}
	return items
}

func Control(cal calendar.Data, lp loops.Data, crons []hermes.Cron) MissionControl {
	events := cal.Events
	failedCount := 0
	pausedCount := 0
	for _, c := range crons {
		if c.LastStatus != "" && c.LastStatus != "ok" {
			failedCount++
		}
		if c.State == "paused" {
			pausedCount++
		}
	}
	loopState := "ok"
	if len(lp.Todos)+len(lp.OpenLoops) > 0 {
		loopState = "warn"
	}
	inboxState := "ok"
	if len(lp.Inbox) > 0 {
		inboxState = "warn"
	}
	droidState := "ok"
	switch {
	case failedCount > 0:
		droidState = "critical"
	case pausedCount > 0:
		droidState = "warn"
	}

	calState := "idle"
	if len(events) > 0 {
		calState = "ok"
	}

	return MissionControl{Commands: []Command{
		{Title: "Review today", Detail: fmt.Sprintf("%d calendar items queued", len(events)), State: calState},
		{Title: "Process loops", Detail: fmt.Sprintf("%d todos/open loops", len(lp.Todos)+len(lp.OpenLoops)), State: loopState},
		{Title: "Clear inbox", Detail: fmt.Sprintf("%d unprocessed captures", len(lp.Inbox)), State: inboxState},
		{Title: "Check droids", Detail: fmt.Sprintf("%d failed / %d paused jobs", failedCount, pausedCount), State: droidState},
	}}
}

func nonEmpty(a, fallback string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return fallback
}

func formatShort(iso string) string {
	t, err := time.Parse(time.RFC3339, strings.Replace(iso, "Z", "+00:00", 1))
	if err != nil {
		if len(iso) > 16 {
			return iso[:16]
		}
		return iso
	}
	return t.Local().Format("02/01 15:04")
}
