// Package loops parses a few specific notes inside the Hermes Obsidian
// folder: Inbox.md (unprocessed captures), Open Loops.md (active loops
// under named sections), and unchecked todo lines across Hermes/*.md.
package loops

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/diogo/hermes-agent-dashboard/internal/util"
	"github.com/diogo/hermes-agent-dashboard/internal/vault"
)

type Item struct {
	Title  string `json:"title"`
	Source string `json:"source"`
}

type Data struct {
	Path       string `json:"path"`
	Present    bool   `json:"present"`
	CheckedAt  string `json:"checked_at"`
	Todos      []Item `json:"todos"`
	OpenLoops  []Item `json:"open_loops"`
	Inbox      []Item `json:"inbox"`
	TaskCount  int    `json:"task_count"`
	LoopCount  int    `json:"loop_count"`
	InboxCount int    `json:"inbox_count"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

var (
	headingRE      = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	bulletRE       = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+`)
	uncheckedTask  = regexp.MustCompile(`^\s*[-*+]\s+\[(?: |todo|TODO)\]\s+(.+?)\s*$`)
	checkedTask    = regexp.MustCompile(`^\s*[-*+]\s+\[[xX]\]\s+`)
	leadingBracket = regexp.MustCompile(`^\[(?: |todo|TODO)\]\s+`)
)

var ignored = map[string]bool{
	"_add new captures below this line._": true,
	"_add active loops here._":            true,
	"add new captures below this line.":   true,
	"add active loops here.":              true,
	"-":                                   true,
}

// Collect scans the Hermes vault subdirectory. The returned Data mirrors
// the shape that get_obsidian_loops() produced in server.py.
func Collect() Data {
	hermes := vault.HermesPath()
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	info, err := os.Stat(hermes)
	d := Data{Path: hermes, CheckedAt: now, Status: "ok"}
	if err != nil || info == nil || !info.IsDir() {
		d.Status = "missing"
		return d
	}
	d.Present = true

	inbox := filepath.Join(hermes, "Inbox.md")
	if _, err := os.Stat(inbox); err == nil {
		d.Inbox = collectBullets(inbox, "Inbox",
			map[string]bool{"unprocessed captures": true}, 5)
	}

	loopsPath := filepath.Join(hermes, "Open Loops.md")
	if _, err := os.Stat(loopsPath); err == nil {
		d.OpenLoops = collectBullets(loopsPath, "Open Loops", map[string]bool{
			"active open loops":       true,
			"urgent / time-sensitive": true,
			"projects":                true,
			"thesis / university":     true,
			"waiting on someone":      true,
		}, 6)
	}

	d.Todos = collectTodosAcross(hermes, 8)
	d.TaskCount = len(d.Todos)
	d.LoopCount = len(d.OpenLoops)
	d.InboxCount = len(d.Inbox)
	return d
}

func collectBullets(path, source string, sections map[string]bool, cap int) []Item {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	allowed := sections == nil
	seen := map[string]bool{}
	var out []Item
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if h := headingRE.FindStringSubmatch(line); len(h) == 3 {
			title := strings.ToLower(util.CompactTitle(h[2], 64))
			allowed = sections == nil || sections[title]
			continue
		}
		if !allowed {
			continue
		}
		if !bulletRE.MatchString(line) {
			continue
		}
		title := taskTitle(line)
		if title == "" {
			title = bulletTitle(line)
		}
		if title == "" {
			continue
		}
		key := strings.ToLower(title)
		if ignored[key] || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Item{Title: title, Source: source})
		if len(out) >= cap {
			break
		}
	}
	return out
}

func collectTodosAcross(hermes string, cap int) []Item {
	entries, err := os.ReadDir(hermes)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	seen := map[string]bool{}
	var out []Item
	for _, n := range names {
		f, err := os.Open(filepath.Join(hermes, n))
		if err != nil {
			continue
		}
		source := strings.TrimSuffix(n, filepath.Ext(n))
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			title := taskTitle(sc.Text())
			if title == "" {
				continue
			}
			key := strings.ToLower(title)
			if ignored[key] || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Item{Title: title, Source: source})
			if len(out) >= cap {
				break
			}
		}
		f.Close()
		if len(out) >= cap {
			break
		}
	}
	return out
}

func taskTitle(line string) string {
	m := uncheckedTask.FindStringSubmatch(line)
	if len(m) < 2 {
		return ""
	}
	return util.CompactTitle(m[1], 84)
}

func bulletTitle(line string) string {
	if checkedTask.MatchString(line) {
		return ""
	}
	t := bulletRE.ReplaceAllString(line, "")
	t = leadingBracket.ReplaceAllString(strings.TrimSpace(t), "")
	return util.CompactTitle(strings.TrimSpace(t), 84)
}
