// Package repo reads local-only git metadata for the dashboard directory.
// It never fetches or contacts remotes.
package repo

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Status struct {
	Branch       string `json:"branch"`
	Head         string `json:"head"`
	Dirty        bool   `json:"dirty"`
	ChangedFiles int    `json:"changed_files"`
	Upstream     string `json:"upstream"`
	Ahead        int    `json:"ahead"`
	Behind       int    `json:"behind"`
}

func Read(ctx context.Context, dir string) Status {
	s := Status{}
	s.Branch = run(ctx, dir, "branch", "--show-current")
	s.Head = run(ctx, dir, "rev-parse", "--short", "HEAD")
	porcelain := run(ctx, dir, "status", "--porcelain")
	if porcelain != "" {
		s.Dirty = true
		for _, line := range strings.Split(porcelain, "\n") {
			if strings.TrimSpace(line) != "" {
				s.ChangedFiles++
			}
		}
	}
	s.Upstream = run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if s.Upstream != "" {
		counts := strings.Fields(run(ctx, dir, "rev-list", "--left-right", "--count", "HEAD..."+s.Upstream))
		if len(counts) == 2 {
			s.Ahead, _ = strconv.Atoi(counts[0])
			s.Behind, _ = strconv.Atoi(counts[1])
		}
	}
	return s
}

func run(ctx context.Context, dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
