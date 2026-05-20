// Package health summarizes connectivity and the freshness of Hermes
// cron state files for the dashboard's service-health panel.
package health

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/diogo/hermes-agent-dashboard/internal/hermes"
)

type Source struct {
	Owner      string `json:"owner"`
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	AgeSeconds *int64 `json:"age_seconds"`
	UpdatedAt  string `json:"updated_at"`
}

type Ping struct {
	Reachable bool     `json:"reachable"`
	LatencyMs *float64 `json:"latency_ms"`
}

type DashboardBackend struct {
	Status    string `json:"status"`
	Port      int    `json:"port"`
	CheckedAt string `json:"checked_at"`
}

type CronHealth struct {
	Status           string   `json:"status"`
	FreshnessSeconds *int64   `json:"freshness_seconds"`
	Sources          []Source `json:"sources"`
}

type Data struct {
	DashboardBackend DashboardBackend `json:"dashboard_backend"`
	HermesCron       CronHealth       `json:"hermes_cron"`
	Internet         Ping             `json:"internet"`
	LocalIP          string           `json:"local_ip"`
}

func Collect(ctx context.Context, port int) Data {
	now := time.Now()
	sources := make([]Source, 0, 2)
	var newestMtime time.Time
	for _, s := range hermes.DefaultCronSources() {
		src := Source{Owner: s.Owner, Path: s.Path}
		if st, err := os.Stat(s.Path); err == nil {
			src.Exists = true
			age := int64(now.Sub(st.ModTime()).Seconds())
			if age < 0 {
				age = 0
			}
			src.AgeSeconds = &age
			src.UpdatedAt = st.ModTime().Format(time.RFC3339)
			if st.ModTime().After(newestMtime) {
				newestMtime = st.ModTime()
			}
		}
		sources = append(sources, src)
	}

	cron := CronHealth{Status: "missing", Sources: sources}
	if !newestMtime.IsZero() {
		freshness := int64(now.Sub(newestMtime).Seconds())
		if freshness < 0 {
			freshness = 0
		}
		cron.Status = "ok"
		cron.FreshnessSeconds = &freshness
	}

	return Data{
		DashboardBackend: DashboardBackend{
			Status:    "ok",
			Port:      port,
			CheckedAt: now.UTC().Format(time.RFC3339),
		},
		HermesCron: cron,
		Internet:   pingInternet(ctx),
		LocalIP:    localIP(),
	}
}

func pingInternet(ctx context.Context) Ping {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", "1.1.1.1").Output()
	if err != nil {
		return Ping{}
	}
	p := Ping{Reachable: true}
	for _, tok := range strings.Fields(string(out)) {
		if rest, ok := strings.CutPrefix(tok, "time="); ok {
			if v, err := strconv.ParseFloat(rest, 64); err == nil {
				p.LatencyMs = &v
				break
			}
		}
	}
	return p
}

func localIP() string {
	conn, err := net.DialTimeout("udp", "1.1.1.1:80", 250*time.Millisecond)
	if err == nil {
		defer conn.Close()
		return strings.Split(conn.LocalAddr().String(), ":")[0]
	}
	if host, err := os.Hostname(); err == nil {
		if addrs, err := net.LookupHost(host); err == nil && len(addrs) > 0 {
			return addrs[0]
		}
	}
	return ""
}
