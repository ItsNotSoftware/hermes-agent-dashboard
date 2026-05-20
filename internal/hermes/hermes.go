// Package hermes reads Hermes agent config and per-profile cron state.
package hermes

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CronSource pairs a profile/owner label with the jobs.json path to read.
type CronSource struct {
	Owner string
	Path  string
}

// DefaultCronSources mirrors CRON_JOB_SOURCES in server.py.
func DefaultCronSources() []CronSource {
	home, _ := os.UserHomeDir()
	return []CronSource{
		{Owner: "C-3PO", Path: filepath.Join(home, ".hermes", "cron", "jobs.json")},
		{Owner: "EVE", Path: filepath.Join(home, ".hermes", "profiles", "eve", "cron", "jobs.json")},
	}
}

type ModelInfo struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
}

// ModelConfig reads the model:{} section of ~/.hermes/config.yaml.
// The original Python parser is intentionally tiny — it only handles a
// flat block of key:value pairs under a `model:` heading. We replicate
// that here instead of pulling in a full YAML library.
func ModelConfig() ModelInfo {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".hermes", "config.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return ModelInfo{}
	}

	var model, provider, baseURL string
	inModel := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			if stripped == "model:" {
				inModel = true
				continue
			}
			if inModel {
				break
			}
			continue
		}
		if !inModel || indent < 2 {
			continue
		}
		key, val, ok := strings.Cut(stripped, ":")
		if !ok {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), "'\"")
		switch key {
		case "default":
			model = val
		case "provider":
			provider = val
		case "base_url":
			baseURL = val
		}
	}

	displayModel := model
	if idx := strings.LastIndex(displayModel, "/"); idx >= 0 {
		displayModel = displayModel[idx+1:]
	}
	return ModelInfo{Model: displayModel, Provider: inferProvider(model, provider, baseURL)}
}

func inferProvider(model, provider, baseURL string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "openai", "openai-codex", "codex":
		return "OpenAI"
	case "openrouter":
		return "OpenRouter"
	case "anthropic":
		return "Anthropic"
	case "google":
		return "Google"
	case "local":
		return "Local"
	case "edge":
		return "Edge"
	}
	if p != "" && p != "auto" {
		return strings.Title(p) //nolint:staticcheck // matches Python title() semantics for this use
	}

	m := strings.ToLower(model)
	u := strings.ToLower(baseURL)
	switch {
	case strings.Contains(u, "chatgpt.com/backend-api/codex"),
		strings.HasPrefix(m, "gpt-"),
		strings.HasPrefix(m, "o1"),
		strings.HasPrefix(m, "o3"):
		return "OpenAI"
	case strings.HasPrefix(m, "openrouter/"):
		return "OpenRouter"
	case strings.HasPrefix(m, "anthropic/"), strings.Contains(m, "claude"):
		return "Anthropic"
	case strings.HasPrefix(m, "google/"), strings.Contains(m, "gemini"):
		return "Google"
	case strings.HasPrefix(m, "local/"):
		return "Local"
	}
	return ""
}

type Cron struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Owner      string `json:"owner"`
	Profile    string `json:"profile"`
	Schedule   string `json:"schedule"`
	State      string `json:"state"` // active | paused
	NextRun    string `json:"next_run"`
	NextRunAt  string `json:"next_run_at"`
	LastRun    string `json:"last_run"`
	LastRunAt  string `json:"last_run_at"`
	LastStatus string `json:"last_status,omitempty"`
	Model      string `json:"model,omitempty"`
}

type cronFile struct {
	Jobs []map[string]any `json:"jobs"`
}

// CronJobs reads all sources and returns a single sorted list. Jobs with a
// next_run_at come first (earliest first), then the rest by insertion order.
func CronJobs(sources []CronSource) []Cron {
	var out []Cron
	for _, src := range sources {
		out = append(out, readCronFile(src)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].NextRunAt, out[j].NextRunAt
		switch {
		case ai != "" && aj == "":
			return true
		case ai == "" && aj != "":
			return false
		default:
			return ai < aj
		}
	})
	return out
}

func readCronFile(src CronSource) []Cron {
	raw, err := os.ReadFile(src.Path)
	if err != nil {
		return nil
	}
	var f cronFile
	if err := json.Unmarshal(raw, &f); err != nil {
		fmt.Fprintf(os.Stderr, "hermes: parse %s: %v\n", src.Path, err)
		return nil
	}
	out := make([]Cron, 0, len(f.Jobs))
	for _, j := range f.Jobs {
		out = append(out, normalizeJob(src.Owner, j))
	}
	return out
}

func normalizeJob(owner string, j map[string]any) Cron {
	scheduleStr := ""
	if sched, ok := j["schedule"].(map[string]any); ok {
		kind, _ := sched["kind"].(string)
		switch kind {
		case "cron", "":
			if d, ok := sched["display"].(string); ok && d != "" {
				scheduleStr = d
			} else if e, ok := sched["expr"].(string); ok {
				scheduleStr = e
			}
		case "once":
			runAt, _ := sched["run_at"].(string)
			if runAt != "" {
				if len(runAt) > 16 {
					runAt = runAt[:16]
				}
				scheduleStr = "once: " + runAt
			} else {
				scheduleStr = "once"
			}
		default:
			if d, ok := sched["display"].(string); ok && d != "" {
				scheduleStr = d
			} else {
				b, _ := json.Marshal(sched)
				scheduleStr = string(b)
			}
		}
	}

	state := "active"
	if s, _ := j["state"].(string); s == "paused" {
		state = "paused"
	}

	nextRunAt, _ := j["next_run_at"].(string)
	nextRun := "N/A"
	if nextRunAt != "" {
		if t, err := time.Parse(time.RFC3339, strings.Replace(nextRunAt, "Z", "+00:00", 1)); err == nil {
			nextRun = t.Local().Format("02/01 - 15:04")
		} else if len(nextRunAt) >= 19 {
			nextRun = nextRunAt[:19]
		}
	}

	lastRunAt, _ := j["last_run_at"].(string)
	if lastRunAt == "" {
		lastRunAt, _ = j["last_finished_at"].(string)
	}
	if lastRunAt == "" {
		lastRunAt, _ = j["last_started_at"].(string)
	}

	lastStatus, _ := j["last_status"].(string)
	id, _ := j["id"].(string)
	name, _ := j["name"].(string)
	if name == "" {
		name = "Unnamed"
	}
	model, _ := j["model"].(string)

	return Cron{
		ID:         id,
		Name:       name,
		Owner:      owner,
		Profile:    owner,
		Schedule:   scheduleStr,
		State:      state,
		NextRun:    nextRun,
		NextRunAt:  nextRunAt,
		LastRun:    formatLocalShort(lastRunAt),
		LastRunAt:  lastRunAt,
		LastStatus: lastStatus,
		Model:      model,
	}
}

func formatLocalShort(iso string) string {
	if iso == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, strings.Replace(iso, "Z", "+00:00", 1))
	if err != nil {
		if len(iso) >= 16 {
			return iso[:16]
		}
		return iso
	}
	return t.Local().Format("02/01 15:04")
}
