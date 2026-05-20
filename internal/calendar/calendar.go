// Package calendar wraps the existing Hermes google_api.py helper to fetch
// upcoming Google Calendar events. The Go dashboard intentionally reuses
// the Python OAuth/credential plumbing already configured for Hermes.
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/diogo/hermes-agent-dashboard/internal/util"
)

type Event struct {
	Title    string `json:"title"`
	Start    string `json:"start"`
	Time     string `json:"time"`
	Day      string `json:"day"`
	Clock    string `json:"clock"`
	Location string `json:"location,omitempty"`
	Status   string `json:"status,omitempty"`
}

type Data struct {
	Source    string  `json:"source"`
	Status    string  `json:"status"` // ok | missing | error | timeout
	CheckedAt string  `json:"checked_at"`
	Events    []Event `json:"events"`
	Error     string  `json:"error,omitempty"`
}

// ScriptPath is the location of the helper script the dashboard shells out to.
func ScriptPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes", "skills", "productivity", "google-workspace", "scripts", "google_api.py")
}

func Upcoming(ctx context.Context) Data {
	script := ScriptPath()
	now := time.Now().UTC().Format(time.RFC3339)
	d := Data{Source: script, CheckedAt: now, Status: "ok", Events: []Event{}}
	if _, err := os.Stat(script); err != nil {
		d.Status = "missing"
		return d
	}

	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", script, "calendar", "list", "--max", "6")
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		d.Status = "timeout"
		d.Error = "calendar list timed out"
		return d
	}
	if err != nil {
		d.Status = "error"
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		d.Error = util.CompactTitle(firstLine(stderr, string(out), err.Error()), 120)
		return d
	}

	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		d.Status = "error"
		d.Error = util.CompactTitle(fmt.Sprintf("parse: %v", err), 120)
		return d
	}
	for i, item := range raw {
		if i >= 6 {
			break
		}
		title := util.CompactTitle(stringOf(item["summary"]), 72)
		if title == "" {
			title = "(no title)"
		}
		start := stringOf(item["start"])
		day, clock, label := timeParts(start)
		d.Events = append(d.Events, Event{
			Title:    title,
			Start:    start,
			Time:     label,
			Day:      day,
			Clock:    clock,
			Location: util.CompactTitle(stringOf(item["location"]), 48),
			Status:   util.CompactTitle(stringOf(item["status"]), 24),
		})
	}
	return d
}

func timeParts(start string) (day, clock, label string) {
	if start == "" {
		return "--", "--", "--"
	}
	if len(start) == 10 {
		// All-day event: YYYY-MM-DD
		if t, err := time.Parse("2006-01-02", start); err == nil {
			day = strings.ToUpper(t.Format("Mon 02/01"))
			return day, "ALL DAY", day + " all day"
		}
	}
	s := strings.Replace(start, "Z", "+00:00", 1)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		local := t.Local()
		day = strings.ToUpper(local.Format("Mon 02/01"))
		clock = local.Format("15:04")
		return day, clock, day + " " + clock
	}
	fallback := start
	if len(fallback) > 16 {
		fallback = fallback[:16]
	}
	day = fallback
	if len(day) > 6 {
		day = day[:6]
	}
	clock = strings.TrimSpace(fallback[len(day):])
	if day == "" {
		day = "--"
	}
	if clock == "" {
		clock = "--"
	}
	return day, clock, fallback
}

func firstLine(parts ...string) string {
	for _, p := range parts {
		for _, line := range strings.Split(p, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				return line
			}
		}
	}
	return ""
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}
