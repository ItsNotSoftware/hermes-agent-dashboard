package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/diogo/hermes-agent-dashboard/internal/store"
	"github.com/diogo/hermes-agent-dashboard/internal/ui"
)

const port = 9200

func main() {
	dashboardDir := flag.String("dir", "", "dashboard directory (defaults to executable dir; used for git status and Claude project lookup)")
	once := flag.Bool("once", false, "fetch a single snapshot, print as JSON, and exit (no UI)")
	wait := flag.Duration("wait", 0, "with --once, wait this duration before snapshotting so rate-based metrics warm up")
	flag.Parse()

	dir := *dashboardDir
	if dir == "" {
		dir = resolveDashboardDir()
	}
	st := store.New(dir, port)

	if *once {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// One synchronous full refresh.
		st.Start(ctx)
		// Give rate-based samplers a second sample point and slow pollers
		// a chance to populate (OpenAI/Claude can take seconds).
		warm := *wait
		if warm == 0 {
			warm = 3 * time.Second
		}
		time.Sleep(warm)
		snap := st.Snapshot()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(snap)
		return
	}

	// Long-running path: start pollers, then run the Fyne UI on the main thread.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	st.Start(ctx)
	ui.New(st).Run(ctx)
}

// resolveDashboardDir picks the directory used for git status and Claude
// project lookup. Order: the directory containing the running binary if it
// is a git repo; the current working directory; otherwise the executable
// directory.
func resolveDashboardDir() string {
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		if isGitRepo(d) {
			return d
		}
	}
	if cwd, err := os.Getwd(); err == nil && isGitRepo(cwd) {
		return cwd
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
